package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type cleanupDocker struct {
	project   string
	container string
	labels    string
	localHost string
	dataPath  string
	commands  [][]string
	events    *[]string
	failOn    string
}

func (d *cleanupDocker) Run(ctx context.Context, name string, args []string, stdout, _ io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if name != "docker" {
		return fmt.Errorf("unexpected executable %q", name)
	}
	copyArgs := append([]string(nil), args...)
	d.commands = append(d.commands, copyArgs)
	command := strings.Join(args, " ")
	if d.events != nil {
		*d.events = append(*d.events, command)
	}
	if d.failOn != "" && strings.Contains(command, d.failOn) {
		return errors.New("fixture command failed")
	}
	switch {
	case strings.Contains(command, "config --format json"):
		_, _ = io.WriteString(stdout, d.project)
	case len(args) > 0 && args[0] == "ps":
		_, _ = io.WriteString(stdout, d.container)
	case len(args) >= 2 && args[0] == "image" && args[1] == "inspect":
		_, _ = io.WriteString(stdout, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	case len(args) > 0 && args[0] == "run":
		return d.runCleanupFixture(args)
	case len(args) > 0 && args[0] == "inspect":
		_, _ = io.WriteString(stdout, d.labels)
	case len(args) > 0 && args[0] == "context":
		_, _ = io.WriteString(stdout, d.localHost)
	}
	return nil
}

func (d *cleanupDocker) runCleanupFixture(args []string) error {
	if d.dataPath == "" {
		return errors.New("cleanup fixture has no PostgreSQL data path")
	}
	for _, required := range []string{
		"--pull=never", "--network=none", "--user=0:0", "--read-only", "--cap-drop=ALL",
		"--cap-add=DAC_OVERRIDE", "--cap-add=FOWNER", "--security-opt=no-new-privileges", "--entrypoint=/bin/sh",
	} {
		if !containsArgument(args, required) {
			return fmt.Errorf("cleanup helper is missing %s", required)
		}
	}
	if countArgument(args, "--mount") != 1 {
		return errors.New("cleanup helper must have one verified parent bind mount")
	}
	mountIndex := argumentIndex(args, "--mount")
	mount := args[mountIndex+1]
	if !strings.Contains(mount, "dst=/cleanup") || !strings.Contains(mount, "bind-recursive=disabled") {
		return errors.New("cleanup helper has an unexpected bind mount")
	}
	expectedSource := "src=" + filepath.Dir(d.dataPath)
	if !strings.Contains(mount, expectedSource) {
		return errors.New("cleanup helper did not bind the verified PostgreSQL parent")
	}
	deleting := strings.Contains(args[len(args)-1], "rm -rf -- /cleanup/data")
	if deleting == strings.Contains(mount, "readonly") {
		return errors.New("cleanup helper bind access does not match its operation")
	}
	if err := validateCleanupFixtureData(d.dataPath); err != nil {
		return err
	}
	if deleting {
		return os.RemoveAll(d.dataPath)
	}
	return nil
}

func validateCleanupFixtureData(dataPath string) error {
	info, err := os.Lstat(dataPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Refusing a symlinked PostgreSQL directory")
	}
	if !info.IsDir() {
		return errors.New("PostgreSQL data path is not a directory")
	}
	entries, err := os.ReadDir(dataPath)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	versionPath := filepath.Join(dataPath, "PG_VERSION")
	versionInfo, versionErr := os.Lstat(versionPath)
	if versionErr != nil || !versionInfo.Mode().IsRegular() {
		versionPath = filepath.Join(dataPath, "18", "docker", "PG_VERSION")
		versionInfo, versionErr = os.Lstat(versionPath)
	}
	if versionErr != nil || !versionInfo.Mode().IsRegular() {
		return errors.New("Refusing data without a supported regular PG_VERSION file")
	}
	return filepath.WalkDir(dataPath, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("Refusing symlinks in PostgreSQL data")
		}
		return nil
	})
}

func containsArgument(args []string, want string) bool {
	return argumentIndex(args, want) >= 0
}

func countArgument(args []string, want string) int {
	count := 0
	for _, arg := range args {
		if arg == want {
			count++
		}
	}
	return count
}

func argumentIndex(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}

func newCleanupProject(t *testing.T) string {
	t.Helper()
	root := newProject(t, basicConfig)
	runtimeDir := filepath.Join(root, "config", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"admin.env", "postgres_environment.env", "rabbitmq_environment.env", "redis_environment.env"} {
		if err := os.WriteFile(filepath.Join(runtimeDir, name), []byte("fixture=value\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func cleanupProjectJSON(t *testing.T, root string, storage bool) string {
	t.Helper()
	services := map[string]any{"api": map[string]any{}}
	volumes := map[string]any{"forcad-config": map[string]any{}}
	if storage {
		services["postgres"] = map[string]any{
			"image": "forcad_postgres:local",
			"volumes": []any{map[string]any{
				"type":   "bind",
				"source": filepath.Join(root, "docker_volumes", "postgres", "data"),
				"target": "/var/lib/postgresql",
			}},
		}
		services["redis"] = map[string]any{}
		services["rabbitmq"] = map[string]any{}
	}
	encoded, err := json.Marshal(map[string]any{
		"name":     "forcad-cleanup-test",
		"services": services,
		"volumes":  volumes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func cleanupRunner(root string, withStorage bool) *cleanupDocker {
	return &cleanupDocker{
		project:   cleanupProjectJSONForRunner(root, withStorage),
		localHost: "unix:///var/run/docker.sock\n",
		dataPath:  filepath.Join(root, "docker_volumes", "postgres", "data"),
	}
}

func cleanupProjectJSONForRunner(root string, storage bool) string {
	services := map[string]any{"api": map[string]any{}}
	volumes := map[string]any{"forcad-config": map[string]any{}}
	if storage {
		services["postgres"] = map[string]any{
			"image": "forcad_postgres:local",
			"volumes": []any{map[string]any{
				"type":   "bind",
				"source": filepath.Join(root, "docker_volumes", "postgres", "data"),
				"target": "/var/lib/postgresql",
			}},
		}
		services["redis"] = map[string]any{}
		services["rabbitmq"] = map[string]any{}
	}
	encoded, _ := json.Marshal(map[string]any{"name": "forcad-cleanup-test", "services": services, "volumes": volumes})
	return string(encoded)
}

func runCleanupFixture(t *testing.T, root string, options MaintenanceOptions, docker *cleanupDocker, reset func(context.Context) error) (string, error) {
	t.Helper()
	var output bytes.Buffer
	err := RunMaintenance(context.Background(), root, options, &output, docker.Run, reset)
	return output.String(), err
}

func TestRunMaintenanceResetStopsWritersBeforeResetAndDown(t *testing.T) {
	root := newCleanupProject(t)
	events := []string{}
	docker := cleanupRunner(root, true)
	docker.events = &events
	reset := func(context.Context) error {
		events = append(events, "reset-data")
		return nil
	}
	output, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true}, docker, reset)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output, "Game state reset\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	stop, up, resetIndex, down := eventIndex(events, " stop "), eventIndex(events, " up "), eventIndex(events, "reset-data"), eventIndex(events, " down ")
	if stop < 0 || stop >= up || up >= resetIndex || resetIndex >= down {
		t.Fatalf("unexpected operation order: %v", events)
	}
	if !strings.Contains(events[up], "--wait --no-deps postgres redis rabbitmq") {
		t.Fatalf("storage services were not started independently: %s", events[up])
	}
	for _, name := range []string{"admin.env", "postgres_environment.env", "rabbitmq_environment.env", "redis_environment.env"} {
		if _, err := os.Stat(filepath.Join(root, "config", "runtime", name)); err != nil {
			t.Fatalf("reset removed generated runtime file %s: %v", name, err)
		}
	}
}

func TestRunMaintenanceRequiresConfirmationBeforeDocker(t *testing.T) {
	root := newCleanupProject(t)
	docker := cleanupRunner(root, true)
	_, err := runCleanupFixture(t, root, MaintenanceOptions{}, docker, func(context.Context) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("unconfirmed maintenance error = %v", err)
	}
	if len(docker.commands) != 0 {
		t.Fatalf("unconfirmed maintenance ran Docker commands: %v", docker.commands)
	}
}

func TestRunMaintenanceResetFailureLeavesProjectStopped(t *testing.T) {
	root := newCleanupProject(t)
	events := []string{}
	docker := cleanupRunner(root, true)
	docker.events = &events
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true}, docker, func(context.Context) error {
		events = append(events, "reset-data")
		return errors.New("fixture reset failure")
	})
	if err == nil || !strings.Contains(err.Error(), "remain stopped") {
		t.Fatalf("reset failure = %v", err)
	}
	if eventIndex(events, "reset-data") < 0 || eventIndex(events, " down ") >= 0 {
		t.Fatalf("failure unexpectedly restarted or removed services: %v", events)
	}
}

func TestRunMaintenanceNormalResetAllowsExternalStorage(t *testing.T) {
	root := newCleanupProject(t)
	events := []string{}
	docker := cleanupRunner(root, false)
	docker.events = &events
	called := false
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true}, docker, func(context.Context) error {
		called = true
		events = append(events, "reset-data")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || eventIndex(events, " up ") >= 0 || eventIndex(events, " down ") < 0 {
		t.Fatalf("unexpected external-storage reset sequence: %v", events)
	}
}

func TestRunMaintenanceCleanRemovesOnlyGeneratedFilesAndPostgresData(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/fixture-docker.sock")
	root := newCleanupProject(t)
	data := filepath.Join(root, "docker_volumes", "postgres", "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "PG_VERSION"), []byte("18\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docker_volumes", "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "checkers"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "checkers", "checker.py"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	events := []string{}
	docker := cleanupRunner(root, true)
	docker.events = &events
	output, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true, Clean: true}, docker, nil)
	if err != nil {
		t.Fatal(err)
	}
	if output != "Local game data and generated runtime files removed\n" {
		t.Fatalf("output = %q", output)
	}
	if eventIndex(events, " down ") < 0 {
		t.Fatalf("clean did not stop Compose project: %v", events)
	}
	helperCalls := matchingEvents(events, "run --rm --pull=never")
	if len(helperCalls) != 2 {
		t.Fatalf("full clean invoked %d cleanup helpers, want preflight and deletion: %v", len(helperCalls), events)
	}
	preflight, down, deletion := helperCalls[0], eventIndex(events, " down "), helperCalls[1]
	if preflight < 0 || preflight >= down || down >= deletion {
		t.Fatalf("cleanup helper order is unsafe: %v", events)
	}
	preflightCommand, deleteCommand := cleanupHelperCommand(docker.commands, 0), cleanupHelperCommand(docker.commands, 1)
	preflightArgs, deleteArgs := strings.Join(preflightCommand, " "), strings.Join(deleteCommand, " ")
	if !strings.Contains(preflightArgs, "bind-recursive=disabled,readonly") || !strings.Contains(deleteArgs, "bind-recursive=disabled") || strings.Contains(deleteArgs, "readonly") {
		t.Fatalf("cleanup helper mount modes are incorrect: preflight=%q delete=%q", preflightArgs, deleteArgs)
	}
	for _, required := range []string{"--network=none", "--pull=never", "--cap-drop=ALL", "--cap-add=DAC_OVERRIDE", "--cap-add=FOWNER", "sha256:aaaaaaaa"} {
		if !strings.Contains(preflightArgs, required) || !strings.Contains(deleteArgs, required) {
			t.Fatalf("cleanup helper commands lack %q: preflight=%q delete=%q", required, preflightArgs, deleteArgs)
		}
	}
	if len(deleteCommand) == 0 || !strings.Contains(deleteCommand[len(deleteCommand)-1], "rm -rf -- /cleanup/data") || strings.Contains(deleteCommand[len(deleteCommand)-1], "docker_volumes") {
		t.Fatalf("cleanup helper is not limited to its fixed in-container data path: %q", deleteCommand)
	}
	if _, err := os.Lstat(data); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PostgreSQL data remains: %v", err)
	}
	for _, name := range []string{"admin.env", "postgres_environment.env", "rabbitmq_environment.env", "redis_environment.env"} {
		if _, err := os.Lstat(filepath.Join(root, "config", "runtime", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("generated runtime file %s remains: %v", name, err)
		}
	}
	for path, want := range map[string]string{
		filepath.Join(root, "config", "config.yml"):       basicConfig,
		filepath.Join(root, "checkers", "checker.py"):     "keep",
		filepath.Join(root, "docker_volumes", "keep.txt"): "keep",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("preserved file %s = %q, error %v", path, got, err)
		}
	}
}

func TestRunMaintenanceFullRejectsExternalStorageBeforeDown(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/fixture-docker.sock")
	root := newCleanupProject(t)
	docker := cleanupRunner(root, false)
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true, Full: true}, docker, nil)
	if err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("external storage full reset error = %v", err)
	}
	if eventIndex(commandEvents(docker.commands), " down ") >= 0 {
		t.Fatal("full reset stopped external-storage project")
	}
}

func TestRunMaintenanceFullRejectsRemoteDockerBeforeDown(t *testing.T) {
	t.Setenv("DOCKER_HOST", "ssh://remote.example")
	root := newCleanupProject(t)
	docker := cleanupRunner(root, true)
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true, Full: true}, docker, nil)
	if err == nil || !strings.Contains(err.Error(), "local Unix-socket") {
		t.Fatalf("remote Docker full reset error = %v", err)
	}
	if eventIndex(commandEvents(docker.commands), " down ") >= 0 {
		t.Fatal("full reset ran Compose down against remote Docker")
	}
}

func TestRunMaintenanceFullRejectsSymlinkedPostgresDataBeforeDown(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/fixture-docker.sock")
	root := newCleanupProject(t)
	outside := filepath.Join(root, "checkers")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "docker_volumes", "postgres", "data")
	if err := os.MkdirAll(filepath.Dir(data), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, data); err != nil {
		t.Fatal(err)
	}
	docker := cleanupRunner(root, true)
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true, Full: true}, docker, nil)
	if err == nil || !strings.Contains(err.Error(), "validate the local PostgreSQL data directory") {
		t.Fatalf("symlinked PostgreSQL data error = %v", err)
	}
	if eventIndex(commandEvents(docker.commands), " down ") >= 0 {
		t.Fatal("full reset stopped Compose before rejecting the symlink")
	}
}

func TestRunMaintenanceFullSanitizesPostDeleteHelperError(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/fixture-docker.sock")
	root := newCleanupProject(t)
	data := filepath.Join(root, "docker_volumes", "postgres", "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "PG_VERSION"), []byte("18\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	events := []string{}
	docker := cleanupRunner(root, true)
	docker.events = &events
	docker.failOn = "rm -rf -- /cleanup/data"
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true, Full: true}, docker, nil)
	if err == nil || strings.Contains(err.Error(), "fixture command failed") || !strings.Contains(err.Error(), "remove the verified local PostgreSQL data directory") {
		t.Fatalf("post-down cleanup error was not sanitized: %v", err)
	}
	if eventIndex(events, " down ") < 0 || len(matchingEvents(events, "run --rm --pull=never")) != 2 {
		t.Fatalf("expected preflight, down, and failed delete helper: %v", events)
	}
	if _, statErr := os.Stat(data); statErr != nil {
		t.Fatalf("failed fake deletion changed fixture data: %v", statErr)
	}
}

func TestRunMaintenanceCleanRejectsGeneratedSymlinkBeforeDocker(t *testing.T) {
	root := newCleanupProject(t)
	path := filepath.Join(root, "config", "runtime", "admin.env")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "config", "config.yml"), path); err != nil {
		t.Fatal(err)
	}
	docker := cleanupRunner(root, true)
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true, Clean: true}, docker, nil)
	if err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("generated symlink error = %v", err)
	}
	if len(docker.commands) != 0 {
		t.Fatalf("clean ran Docker before rejecting generated symlink: %v", docker.commands)
	}
}

func TestRunMaintenanceRejectsForeignComposeProject(t *testing.T) {
	root := newCleanupProject(t)
	docker := cleanupRunner(root, true)
	docker.container = "abc123\n"
	docker.labels = "{\"com.docker.compose.project.working_dir\":\"/other/project/deployment/docker\"}\n"
	_, err := runCleanupFixture(t, root, MaintenanceOptions{Confirm: true}, docker, func(context.Context) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "another directory") {
		t.Fatalf("foreign project error = %v", err)
	}
	if eventIndex(commandEvents(docker.commands), " stop ") >= 0 {
		t.Fatal("maintenance stopped a foreign Compose project")
	}
}

func TestRunMaintenanceRejectsChangedPostgresMount(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/fixture-docker.sock")
	root := newCleanupProject(t)
	docker := cleanupRunner(root, true)
	project := map[string]any{}
	if err := json.Unmarshal([]byte(docker.project), &project); err != nil {
		t.Fatal(err)
	}
	services := project["services"].(map[string]any)
	postgres := services["postgres"].(map[string]any)
	mounts := postgres["volumes"].([]any)
	mounts[0].(map[string]any)["source"] = "/outside/data"
	encoded, err := json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	docker.project = string(encoded)
	_, err = runCleanupFixture(t, root, MaintenanceOptions{Confirm: true, Full: true}, docker, nil)
	if err == nil || !strings.Contains(err.Error(), "standard docker_volumes") {
		t.Fatalf("changed mount error = %v", err)
	}
	if eventIndex(commandEvents(docker.commands), " down ") >= 0 {
		t.Fatal("full reset proceeded with a nonstandard storage mount")
	}
}

func TestHasMountedPathRejectsNestedMountAndDecodesEscapes(t *testing.T) {
	data := "/tmp/game data/docker_volumes/postgres/data"
	mountInfo := []byte("36 25 0:32 / " + strings.ReplaceAll(data, " ", "\\040") + "/nested rw - ext4 /dev/sda rw\n")
	if err := hasMountedPath(data, mountInfo); err == nil {
		t.Fatal("mount nested below PostgreSQL data was accepted")
	}
	if err := hasMountedPath(data, []byte("36 25 0:32 / /tmp rw - ext4 /dev/sda rw\n")); err != nil {
		t.Fatalf("ancestor mount was rejected: %v", err)
	}
}

func TestUnescapeMountPath(t *testing.T) {
	got, err := unescapeMountPath("/data/a\\040b\\134c")
	if err != nil || got != `/data/a b\c` {
		t.Fatalf("unescapeMountPath = %q, %v", got, err)
	}
	if _, err := unescapeMountPath("/bad\\999"); err == nil {
		t.Fatal("unknown mount escape was accepted")
	}
}

func eventIndex(events []string, fragment string) int {
	for i, event := range events {
		if strings.Contains(event, fragment) {
			return i
		}
	}
	return -1
}

func commandEvents(commands [][]string) []string {
	events := make([]string, len(commands))
	for i, command := range commands {
		events[i] = strings.Join(command, " ")
	}
	return events
}

func matchingEvents(events []string, fragment string) []int {
	var matches []int
	for i, event := range events {
		if strings.Contains(event, fragment) {
			matches = append(matches, i)
		}
	}
	return matches
}

func cleanupHelperCommand(commands [][]string, occurrence int) []string {
	seen := 0
	for _, command := range commands {
		if len(command) > 0 && command[0] == "run" {
			if seen == occurrence {
				return command
			}
			seen++
		}
	}
	return nil
}

func TestPresentStorageServicesUsesStableOrder(t *testing.T) {
	got := presentStorageServices(map[string]composeService{"redis": {}, "postgres": {}, "api": {}})
	if want := []string{"postgres", "redis"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("storage services = %v, want %v", got, want)
	}
}

func TestCleanupProjectJSONIncludesSupportedMount(t *testing.T) {
	root := newCleanupProject(t)
	var project composeProject
	if err := json.Unmarshal([]byte(cleanupProjectJSON(t, root, true)), &project); err != nil {
		t.Fatal(err)
	}
	if err := verifyFullResetSupport(project); err != nil {
		t.Fatal(err)
	}
	if err := verifyPostgresMount(project, filepath.Join(root, "docker_volumes", "postgres", "data")); err != nil {
		t.Fatal(err)
	}
}

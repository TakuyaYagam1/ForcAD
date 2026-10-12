package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
)

const maintenanceProjectConfig = `game:
  round_time: 60
  flag_lifetime: 5
  start_time: 2027-01-01 12:00:00
  timezone: UTC
tasks:
  - name: example
    checker: example/checker.py
teams:
  - ip: 192.0.2.10
    name: Example
    token: "0123456789abcdef"
`

type maintenanceRunnerFixture struct {
	commands [][]string
	events   *[]string
	project  string
	failOn   string
}

func (r *maintenanceRunnerFixture) run(ctx context.Context, name string, args []string, stdout, _ io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if name != "docker" {
		return errors.New("unexpected command")
	}
	command := append([]string(nil), args...)
	r.commands = append(r.commands, command)
	joined := strings.Join(command, " ")
	if r.events != nil {
		*r.events = append(*r.events, joined)
	}
	if r.failOn != "" && strings.Contains(joined, r.failOn) {
		return errors.New("fixture Docker failure")
	}
	if strings.Contains(joined, "config --format json") {
		_, _ = io.WriteString(stdout, r.project)
	} else if len(command) >= 2 && command[0] == "image" && command[1] == "inspect" {
		_, _ = io.WriteString(stdout, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	}
	return nil
}

func newMaintenanceCommandProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, "config", "runtime"),
		filepath.Join(root, "deployment", "docker"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.yml"), []byte(maintenanceProjectConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deployment", "docker", "compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"admin.env", "postgres_environment.env", "rabbitmq_environment.env", "redis_environment.env"} {
		if err := os.WriteFile(filepath.Join(root, "config", "runtime", name), []byte("fixture=value\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func maintenanceProjectJSON(root string, storage bool) string {
	services := map[string]any{"api": map[string]any{}}
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
	encoded, _ := json.Marshal(map[string]any{"name": "forcad-maintenance-test", "services": services})
	return string(encoded)
}

func newMaintenanceRunner(root string, storage bool) *maintenanceRunnerFixture {
	return &maintenanceRunnerFixture{project: maintenanceProjectJSON(root, storage)}
}

func TestRunMaintenanceCommandResetUsesComposeNetworkAfterStoppingWriters(t *testing.T) {
	root := newMaintenanceCommandProject(t)
	events := []string{}
	runner := newMaintenanceRunner(root, true)
	runner.events = &events
	err := runMaintenanceCommand(context.Background(), "reset", []string{"--confirm", "--project-root", root}, io.Discard, runner.run, func() (string, error) {
		t.Fatal("working directory lookup should not run with --project-root")
		return "", nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	stop, up, reset, down := maintenanceEventIndex(events, " stop "), maintenanceEventIndex(events, " up "), maintenanceEventIndex(events, " run --rm --no-deps --entrypoint forcad initializer reset-data --confirm"), maintenanceEventIndex(events, " down ")
	if stop < 0 || stop >= up || up >= reset || reset >= down {
		t.Fatalf("unexpected command order: %v", events)
	}
	if !strings.Contains(events[reset], "--project-directory "+filepath.Join(root, "deployment", "docker")) {
		t.Fatalf("reset-data command did not use the project Compose network: %s", events[reset])
	}
}

func TestRunMaintenanceCommandResetFailureLeavesServicesStopped(t *testing.T) {
	root := newMaintenanceCommandProject(t)
	events := []string{}
	runner := newMaintenanceRunner(root, true)
	runner.events = &events
	runner.failOn = "run --rm --no-deps --entrypoint forcad initializer reset-data"
	err := runMaintenanceCommand(context.Background(), "reset", []string{"--confirm", "--project-root", root}, io.Discard, runner.run, func() (string, error) { return root, nil }, nil)
	if err == nil || !strings.Contains(err.Error(), "remain stopped") {
		t.Fatalf("reset failure = %v", err)
	}
	if maintenanceEventIndex(events, " down ") >= 0 {
		t.Fatalf("failed reset ran compose down: %v", events)
	}
}

func TestRunMaintenanceCommandDefaultsProjectRootToWorkingDirectory(t *testing.T) {
	root := newMaintenanceCommandProject(t)
	runner := newMaintenanceRunner(root, false)
	called := false
	err := runMaintenanceCommand(context.Background(), "reset", []string{"--confirm"}, io.Discard, runner.run, func() (string, error) {
		called = true
		return root, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("working directory was not used")
	}
}

func TestRunMaintenanceCommandRequiresConfirmationBeforeRunner(t *testing.T) {
	root := newMaintenanceCommandProject(t)
	runner := newMaintenanceRunner(root, true)
	err := runMaintenanceCommand(context.Background(), "reset", []string{"--project-root", root}, io.Discard, runner.run, func() (string, error) { return root, nil }, nil)
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("missing confirmation error = %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("missing confirmation invoked Docker: %v", runner.commands)
	}
}

func TestRunMaintenanceCommandCleanUsesFullResetWithoutResetData(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/fixture-docker.sock")
	root := newMaintenanceCommandProject(t)
	runner := newMaintenanceRunner(root, true)
	err := runMaintenanceCommand(context.Background(), "clean", []string{"--confirm", "--project-root", root}, io.Discard, runner.run, func() (string, error) { return root, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if maintenanceEventIndex(maintenanceCommandEvents(runner.commands), " run ") >= 0 {
		t.Fatal("clean invoked the normal reset-data command")
	}
}

func maintenanceEventIndex(events []string, fragment string) int {
	for i, event := range events {
		if strings.Contains(event, fragment) {
			return i
		}
	}
	return -1
}

func maintenanceCommandEvents(commands [][]string) []string {
	events := make([]string, len(commands))
	for i, command := range commands {
		events[i] = strings.Join(command, " ")
	}
	return events
}

func TestRunMaintenanceCommandRejectsCleanFullAndUnexpectedArguments(t *testing.T) {
	root := newMaintenanceCommandProject(t)
	runner := newMaintenanceRunner(root, true)
	for _, args := range [][]string{{"--confirm", "--full", "--project-root", root}, {"--confirm", "--project-root", root, "extra"}} {
		err := runMaintenanceCommand(context.Background(), "clean", args, io.Discard, runner.run, func() (string, error) { return root, nil }, nil)
		if err == nil {
			t.Fatalf("clean accepted unexpected arguments %v", args)
		}
	}
}

func TestRunResetDataCommandUsesOnlyContainerEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://game:secret@db.internal:5432/forcad?sslmode=disable")
	t.Setenv("REDIS_ADDR", "cache.internal:6379")
	t.Setenv("REDIS_PASSWORD", "cache-secret")
	t.Setenv("REDIS_DB", "5")
	called := false
	var output bytes.Buffer
	err := runMaintenanceCommand(context.Background(), "reset-data", []string{"--confirm"}, &output, nil, nil, func(ctx context.Context, cfg config.Config) error {
		called = true
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("reset-data storage context has no deadline")
		}
		if cfg.DatabaseURL != "postgres://game:secret@db.internal:5432/forcad?sslmode=disable" {
			t.Fatalf("database config was not loaded from container env: %q", cfg.DatabaseURL)
		}
		if cfg.RedisAddr != "cache.internal:6379" || cfg.RedisPassword != "cache-secret" || cfg.RedisDB != 5 {
			t.Fatalf("Redis config was not loaded from container env: %+v", cfg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || output.String() != "Game state reset\n" {
		t.Fatalf("reset-data call/output = %v, %q", called, output.String())
	}
}

type maintenanceErrorWriter struct{ err error }

func (w maintenanceErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunResetDataCommandWrapsOutputError(t *testing.T) {
	writeErr := errors.New("output unavailable")
	err := runResetDataCommand(context.Background(), []string{"--confirm"}, maintenanceErrorWriter{err: writeErr}, func(context.Context, config.Config) error {
		return nil
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("reset-data output error = %v, want writer cause", err)
	}
	if !strings.Contains(err.Error(), "bootstrap.runResetDataCommand - write reset result") {
		t.Fatalf("reset-data output error has no function context: %v", err)
	}
}

func TestRunResetDataRequiresConfirmationAndRejectsFullFlags(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://game:secret@db.internal:5432/forcad")
	called := false
	resetter := func(context.Context, config.Config) error { called = true; return nil }
	for _, args := range [][]string{{}, {"--confirm", "--full"}, {"--confirm", "--clean"}, {"--confirm", "extra"}} {
		err := runMaintenanceCommand(context.Background(), "reset-data", args, io.Discard, nil, nil, resetter)
		if err == nil {
			t.Fatalf("reset-data accepted %v", args)
		}
	}
	if called {
		t.Fatal("invalid reset-data invocation ran the storage reset")
	}
}

func TestRunResetDataCommandFailureDoesNotReportSuccess(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://game:secret@db.internal:5432/forcad")
	var output bytes.Buffer
	err := runMaintenanceCommand(context.Background(), "reset-data", []string{"--confirm"}, &output, nil, nil, func(context.Context, config.Config) error {
		return errors.New("fixture failure")
	})
	if err == nil || output.Len() != 0 {
		t.Fatalf("failed reset-data result = %v, output %q", err, output.String())
	}
}

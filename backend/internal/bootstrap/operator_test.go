package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
)

const operatorProjectConfig = "game:\n  round_time: 60\n  flag_lifetime: 5\n  start_time: 2027-01-01 12:00:00\n  timezone: UTC\ntasks:\n  - name: example\n    checker: example/checker.py\nteams:\n  - ip: 192.0.2.10\n    name: Example\n    token: \"0123456789abcdef\"\n"

func TestRunOperatorCommandForwardsComposeArgsAndStdio(t *testing.T) {
	t.Setenv("TEST", "")
	root := t.TempDir()
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "operator-test")
	var output bytes.Buffer
	var gotName string
	var gotArgs []string
	var gotStdin io.Reader
	var gotStdout, gotStderr io.Writer
	runner := func(commandCtx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
		if commandCtx != ctx {
			t.Fatal("Compose runner did not receive the command context")
		}
		gotName, gotArgs = name, append([]string(nil), args...)
		gotStdin, gotStdout, gotStderr = stdin, stdout, stderr
		return nil
	}

	err := runOperatorCommand(ctx, "compose", []string{"--project-root", root, "up", "-d", "--build"}, &output, runner, func() (string, error) {
		t.Fatal("working directory lookup should not run when --project-root is supplied")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{
		"compose",
		"--project-directory", filepath.Join(root, "deployment", "docker"),
		"-f", filepath.Join(root, "deployment", "docker", "compose.yml"),
		"up", "-d", "--build",
	}
	if gotName != "docker" || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("runner received %q %v, want docker %v", gotName, gotArgs, wantArgs)
	}
	if gotStdin != os.Stdin || gotStdout != &output || gotStderr != os.Stderr {
		t.Fatal("Compose runner did not receive the command context and configured stdio")
	}
}

func TestRunOperatorCommandUsesWorkingDirectoryAndTestCompose(t *testing.T) {
	t.Setenv("TEST", "1")
	root := t.TempDir()
	var gotArgs []string
	runner := func(_ context.Context, _ string, args []string, _ io.Reader, _, _ io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	err := runOperatorCommand(context.Background(), "compose", []string{"config"}, io.Discard, runner, func() (string, error) {
		return root, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{
		"compose",
		"--project-directory", filepath.Join(root, "deployment", "docker"),
		"-f", filepath.Join(root, "deployment", "docker", "compose.yml"),
		"-f", filepath.Join(root, "deployment", "docker", "compose.tests.yml"),
		"config",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("Compose args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestRunOperatorCommandPreservesComposeArguments(t *testing.T) {
	t.Setenv("TEST", "")
	root := t.TempDir()
	var gotArgs []string
	runner := func(_ context.Context, _ string, args []string, _ io.Reader, _, _ io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	commandArgs := []string{"up", "--project-root", "compose-option", "--build"}
	err := runOperatorCommand(context.Background(), "compose", commandArgs, io.Discard, runner, func() (string, error) {
		return root, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTail := []string{"up", "--project-root", "compose-option", "--build"}
	if !reflect.DeepEqual(gotArgs[len(gotArgs)-len(wantTail):], wantTail) {
		t.Fatalf("Compose command args = %v, want suffix %v", gotArgs, wantTail)
	}
}

func TestRunOperatorCommandPassesCancellationToComposeRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	runner := func(ctx context.Context, _ string, _ []string, _ io.Reader, _, _ io.Writer) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan error, 1)
	root := t.TempDir()
	go func() {
		done <- runOperatorCommand(ctx, "compose", []string{"up"}, io.Discard, runner, func() (string, error) {
			return root, nil
		})
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Compose command error = %v, want context.Canceled", err)
	} else if !strings.Contains(err.Error(), "bootstrap.runOperatorCommand - run Docker Compose command") {
		t.Fatalf("canceled Compose command has no function context: %v", err)
	}
}

func TestRunOperatorCommandSetupUsesProjectRootAndStorageFlags(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	composeDir := filepath.Join(root, "deployment", "docker")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(composeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(operatorProjectConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(composeDir, "compose.yml"), []byte("services:\n  api: {}\n  redis: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := RunOperatorCommand(context.Background(), "setup", []string{
		"--project-root", root, "--redis", "cache.example:6380",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != "Setup complete\n" {
		t.Fatalf("setup output = %q", output.String())
	}
	document, err := config.LoadDocument(filepath.Join(configDir, "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if document.Storages.Redis.Host != "cache.example" || document.Storages.Redis.Port != 6380 {
		t.Fatalf("setup did not apply external Redis flags: %+v", document.Storages.Redis)
	}
	if _, err := os.Stat(filepath.Join(configDir, "runtime", "compose.external.yml")); err != nil {
		t.Fatalf("setup did not create the external Compose override: %v", err)
	}
}

func TestRunOperatorCommandRejectsCanceledSetupBeforeWriting(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunOperatorCommand(ctx, "setup", []string{"--project-root", root}, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled setup error = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "bootstrap.runOperatorCommand - operator command canceled") {
		t.Fatalf("canceled setup has no function context: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "config")); !os.IsNotExist(err) {
		t.Fatalf("canceled setup modified project root: %v", err)
	}
}

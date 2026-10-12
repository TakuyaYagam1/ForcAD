package operator_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlLauncherPreservesArgumentsAndFindsProject(t *testing.T) {
	root, path := launcherFixture(t)
	writeExecutable(t, filepath.Join(path, "make"), "#!/bin/sh\nprintf 'build log\\n'\n")
	writeExecutable(t, filepath.Join(root, "backend", "bin", "forcad"), "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\"\nexit 17\n")
	command := exec.CommandContext(t.Context(), filepath.Join(root, "control.sh"), "rd", "exec", "api", "a b; $(false)")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "PATH="+path+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 17 {
		t.Fatalf("launcher exit = %v, want 17; stderr = %s", err, stderr.String())
	}
	want := strings.Join([]string{root, "control", "--project-root", root, "rd", "exec", "api", "a b; $(false)", ""}, "\n")
	if string(output) != want || stderr.String() != "build log\n" {
		t.Fatalf("stdout = %q, stderr = %q", output, stderr.String())
	}
}

func TestControlLauncherDoesNotRunStaleBinaryAfterBuildFailure(t *testing.T) {
	root, path := launcherFixture(t)
	writeExecutable(t, filepath.Join(path, "make"), "#!/bin/sh\nexit 23\n")
	writeExecutable(t, filepath.Join(root, "backend", "bin", "forcad"), "#!/bin/sh\necho 'must not run'\n")
	command := exec.CommandContext(t.Context(), filepath.Join(root, "control.sh"), "start")
	command.Env = append(os.Environ(), "PATH="+path+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 || len(output) != 0 {
		t.Fatalf("failed build: output = %q, error = %v", output, err)
	}
}

func launcherFixture(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "game directory")
	path := filepath.Join(root, "backend", "bin")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "control.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(root, "control.sh"), string(source))
	return root, path
}

func writeExecutable(t *testing.T, path, source string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(source), 0o700); err != nil {
		t.Fatal(err)
	}
}

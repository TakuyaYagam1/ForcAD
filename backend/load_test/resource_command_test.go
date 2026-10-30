package load_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func resourceCommandFixture(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResourceCommandProcessFixture$")
	cmd.Env = append(os.Environ(), "FORCAD_RESOURCE_COMMAND_FIXTURE="+mode)
	return cmd
}

func TestResourceCommandProcessFixture(t *testing.T) {
	switch os.Getenv("FORCAD_RESOURCE_COMMAND_FIXTURE") {
	case "":
		return
	case "failure":
		fmt.Fprintln(os.Stderr, "docker stats fixture failed")
		os.Exit(7)
	case "large_failure":
		fmt.Fprint(os.Stderr, "stderr-first ", strings.Repeat("x", 128<<10), " stderr-last")
		os.Exit(9)
	case "timeout":
		fmt.Fprintln(os.Stderr, "probe waiting")
		time.Sleep(30 * time.Second)
	case "success":
		fmt.Println("complete sample")
	default:
		t.Fatal("unknown resource command fixture mode")
	}
	os.Exit(0)
}

func TestResourceCommandFailurePreservesStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := resourceCommandOutput(ctx, resourceCommandFixture(ctx, "failure"))
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("command exit failure was lost: %v", err)
	}
	if !strings.Contains(err.Error(), "docker stats fixture failed") ||
		!strings.Contains(err.Error(), "after ") || !strings.Contains(err.Error(), "context=<nil>") {
		t.Fatalf("command failure diagnostics were lost: %v", err)
	}
}

func TestResourceCommandTimeoutPreservesContextAndStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	cmd := resourceCommandFixture(ctx, "timeout")
	started := time.Now()
	_, err := resourceCommandOutput(ctx, cmd)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "probe waiting") {
		t.Fatalf("timeout cause or stderr was lost: %v", err)
	}
	if cmd.ProcessState == nil || time.Since(started) > 4*time.Second {
		t.Fatalf("timed-out command was not reaped within the cleanup budget: %v", err)
	}
}

func TestResourceCommandFailureBoundsStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := resourceCommandOutput(ctx, resourceCommandFixture(ctx, "large_failure"))
	if err == nil || !strings.Contains(err.Error(), "stderr-first") || !strings.Contains(err.Error(), "stderr-last") {
		t.Fatalf("stderr prefix or suffix was lost: %v", err)
	}
	if len(err.Error()) > 70<<10 {
		t.Fatalf("stderr capture was not bounded: %d bytes", len(err.Error()))
	}
}

func TestResourceCommandPreservesSuccessfulOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	output, err := resourceCommandOutput(ctx, resourceCommandFixture(ctx, "success"))
	if err != nil || string(output) != "complete sample\n" {
		t.Fatalf("successful output = %q, error = %v", output, err)
	}
}

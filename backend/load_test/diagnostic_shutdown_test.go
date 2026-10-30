package load_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func diagnosticFixture(ctx context.Context, mode, pidFile string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDiagnosticStreamProcessFixture$")
	cmd.Env = append(os.Environ(), "FORCAD_DIAGNOSTIC_FIXTURE="+mode, "FORCAD_DIAGNOSTIC_PID_FILE="+pidFile)
	return cmd
}

func TestDiagnosticStreamProcessFixture(t *testing.T) {
	switch os.Getenv("FORCAD_DIAGNOSTIC_FIXTURE") {
	case "":
		return
	case "holder":
		time.Sleep(15 * time.Second)
	case "parent":
		child := diagnosticFixture(context.Background(), "holder", "")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("FORCAD_DIAGNOSTIC_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			_ = child.Process.Kill()
			t.Fatal(err)
		}
		fmt.Println("ready")
		time.Sleep(15 * time.Second)
	case "blocked":
		fmt.Println("sample")
		time.Sleep(15 * time.Second)
	case "finite":
		fmt.Println("first\nsecond")
	default:
		t.Fatal("unknown fixture mode")
	}
	os.Exit(0)
}

func TestDiagnosticStreamCancellationClosesInheritedPipe(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "holder.pid")
	cmd := diagnosticFixture(ctx, "parent", pidFile)
	ready, finished := make(chan struct{}), make(chan struct{})
	go func() {
		_ = collectDiagnosticLines(ctx, cmd, func(line string) error {
			if line == "ready" {
				close(ready)
			}
			return nil
		})
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, err := strconv.Atoi(string(data))
			if err == nil {
				if child, err := os.FindProcess(pid); err == nil {
					_ = child.Kill()
					_ = child.Release()
				}
			}
		}
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("collector did not finish after fixture cleanup")
		}
	})
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not retain its inherited writer")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancellation left Scan blocked on the inherited stdout writer")
	}
	if cmd.ProcessState == nil {
		t.Fatal("collector returned without reaping its command")
	}
}

func TestDiagnosticStreamReportsConsumerFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := diagnosticFixture(ctx, "blocked", "")
	want := errors.New("sample destination failed")
	err := collectDiagnosticLines(ctx, cmd, func(string) error { return want })
	if !errors.Is(err, want) || cmd.ProcessState == nil {
		t.Fatalf("consumer failure was lost or command not reaped: %v", err)
	}
}

func TestDiagnosticStreamPreservesCompleteOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var lines []string
	err := collectDiagnosticLines(ctx, diagnosticFixture(ctx, "finite", ""), func(line string) error {
		lines = append(lines, line)
		return nil
	})
	if err != nil || strings.Join(lines, "\n") != "first\nsecond" {
		t.Fatalf("complete stream = %q, error = %v", lines, err)
	}
}

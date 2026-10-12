//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package checker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerTimeoutStopsCheckerProcessGroup(t *testing.T) {
	executable, executableDir := testExecutable(t)
	heartbeat := filepath.Join(t.TempDir(), "child-heartbeat")
	t.Setenv(fixtureModeEnv, "spawn")
	t.Setenv(fixtureHeartbeatEnv, heartbeat)

	runner := testRunner(0)
	runner.options.TerminateTimeout = 75 * time.Millisecond
	started := time.Now()
	result, err := runner.Run(context.Background(), Request{
		Executable: executable,
		Action:     ActionCheck,
		TeamIP:     "192.0.2.1",
		Timeout:    150 * time.Millisecond,
		EnvPath:    executableDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || !result.Killed || time.Since(started) > 2*time.Second {
		t.Fatalf("process group was not stopped promptly: elapsed=%s result=%+v", time.Since(started), result)
	}

	childHeartbeat := heartbeat + ".child"
	var before []byte
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		before, err = os.ReadFile(childHeartbeat)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("fixture child did not start: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	after, err := os.ReadFile(childHeartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("checker child kept running after its process group was stopped")
	}
}

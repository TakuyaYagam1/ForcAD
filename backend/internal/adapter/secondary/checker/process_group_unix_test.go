//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package checker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerTimeoutStopsCheckerProcessGroup(t *testing.T) {
	for _, mode := range []string{"spawn", "spawn-parent-term"} {
		t.Run(mode, func(t *testing.T) {
			testRunnerStopsProcessGroup(t, mode)
		})
	}
}

func testRunnerStopsProcessGroup(t *testing.T, mode string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := filepath.Join(t.TempDir(), "child-heartbeat")
	t.Setenv(fixtureModeEnv, mode)
	t.Setenv(fixtureHeartbeatEnv, heartbeat)

	process, err := startChecker(executable, []string{"check", "192.0.2.1"}, os.Environ(), DefaultOptions().MaxOutputBytes)
	if err != nil {
		t.Fatal(err)
	}
	// The child only writes after installing its TERM handler. Do not charge
	// process startup on a busy builder against the timeout under test.
	childHeartbeat := heartbeat + ".child"
	startupDeadline := time.Now().Add(10 * time.Second)
	var readyErr error
	for {
		_, readyErr = os.Stat(childHeartbeat)
		if !os.IsNotExist(readyErr) || time.Now().After(startupDeadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	var result Result
	wait := waitForChecker(t.Context(), 150*time.Millisecond, 75*time.Millisecond, process, &result)
	result, err = finishCheckerResult(Request{
		Action: ActionCheck,
	}, result, wait)
	if readyErr != nil {
		t.Fatalf("fixture child did not start: %v", readyErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || !result.Killed || !result.ProcessReaped || !result.OutputComplete || time.Since(started) > 2*time.Second {
		t.Fatalf("process group was not stopped promptly: elapsed=%s result=%+v", time.Since(started), result)
	}

	before, err := os.ReadFile(childHeartbeat)
	if err != nil {
		t.Fatal(err)
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

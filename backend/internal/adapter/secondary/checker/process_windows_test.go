//go:build windows

package checker

import (
	"os"
	"os/exec"
	"testing"
)

func TestKillAfterProcessWaitIsHarmless(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), executable)
	cmd.Env = setEnvironmentValue(os.Environ(), fixtureModeEnv, "exit")
	cmd.Env = setEnvironmentValue(cmd.Env, fixtureExitEnv, "0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := killProcess(cmd); err != nil {
		t.Fatalf("process completion racing with shutdown reported a kill failure: %v", err)
	}
}

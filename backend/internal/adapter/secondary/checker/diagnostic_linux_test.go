//go:build linux

package checker

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessSnapshotExcludesCommandSecretsAndBoundsFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "123")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields("S 12 123 7 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 987654")
	files := map[string]string{
		"stat":    "123 (FLAG_SECRET_COMM (nested)) " + strings.Join(fields, " "),
		"status":  "Name:\tFLAG_SECRET_NAME\nPid:\t123\nPPid:\t12\nThreads:\t1\nVmRSS:\t2048 kB\nNSpid:\t123 7\nSigPnd:\t00000004\nSigBlk:\tFLAG_SECRET_BAD_FIELD\n",
		"cmdline": "FLAG_SECRET_ARG", "environ": "FLAG_SECRET_ENV",
		"wchan": strings.Repeat("w", 400), "stack": strings.Repeat("k", 4096), "syscall": strings.Repeat("0", 2048),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := collectProcessSnapshotAt(root, 123)
	encoded, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(encoded), "FLAG_SECRET") {
		t.Fatalf("process secrets escaped allowlist: %s, %v", encoded, err)
	}
	if snapshot.PPID != 12 || snapshot.PGID != 123 || snapshot.Session != 7 || snapshot.State != "S" || snapshot.StartTimeTicks != 987654 || len(snapshot.Errors) != 0 {
		t.Fatalf("process identity/state lost: %+v", snapshot)
	}
	if snapshot.Status["VmRSS"] != "2048 kB" || snapshot.Status["NSpid"] != "123 7" || snapshot.Status["SigBlk"] != "" ||
		!snapshot.Truncated || len(snapshot.Wchan) != 256 || len(snapshot.KernelStack) != 2048 || len(snapshot.Syscall) != 1024 {
		t.Fatalf("allowlist or output bound failed: %+v", snapshot)
	}
}

func TestProcessSignalRetainsESRCHWithoutChangingCleanup(t *testing.T) {
	// Linux pid_max is at most 2^22, so this cannot target a live process group.
	command := &exec.Cmd{Process: &os.Process{Pid: math.MaxInt32}}
	for _, signal := range []func(*exec.Cmd) (ProcessSignal, error){observeTerminateProcess, func(cmd *exec.Cmd) (ProcessSignal, error) {
		return observeKillProcess(cmd, false)
	}} {
		observed, err := signal(command)
		if err != nil || observed.Outcome != "ESRCH" || observed.Error == "" || observed.PID != math.MaxInt32 || observed.PGID != math.MaxInt32 || observed.Target != "process_group" {
			t.Fatalf("raw signal outcome lost or cleanup changed: %+v, %v", observed, err)
		}
	}
}

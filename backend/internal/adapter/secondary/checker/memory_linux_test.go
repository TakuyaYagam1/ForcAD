//go:build linux

package checker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunnerAppliesLinuxMemoryLimit(t *testing.T) {
	if _, err := exec.LookPath("prlimit"); err != nil {
		t.Skip("prlimit is unavailable")
	}
	checkerPath := filepath.Join(t.TempDir(), "checker")
	if err := os.WriteFile(checkerPath, []byte("#!/bin/sh\nulimit -v\nexit 101\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := NewRunner(Options{MemoryLimitMB: 128}).Run(context.Background(), Request{
		Executable: checkerPath,
		Action:     ActionCheck,
		TeamIP:     "192.0.2.1",
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusUp {
		t.Fatalf("status = %d, want %d; private output=%q", result.Status, StatusUp, result.PrivateMessage)
	}
	limitKB, err := strconv.Atoi(strings.TrimSpace(result.PublicMessage))
	if err != nil {
		t.Fatalf("invalid shell memory limit %q: %v", result.PublicMessage, err)
	}
	if limitKB > 128*1024 || limitKB <= 0 {
		t.Fatalf("virtual memory limit = %d KiB, want at most %d KiB", limitKB, 128*1024)
	}
}

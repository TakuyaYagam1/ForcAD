//go:build !windows

package operator

import (
	"os"
	"testing"
)

func assertPrivateMode(t *testing.T, path string, mode os.FileMode) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode is %04o, want %04o", path, info.Mode().Perm(), mode)
	}
	return info
}

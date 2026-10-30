//go:build !linux

package checker

func collectProcessSnapshot(pid int) *ProcessSnapshot {
	return &ProcessSnapshot{PID: pid, Errors: map[string]string{"proc": "unsupported_platform"}}
}

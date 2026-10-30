//go:build linux

package checker

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func collectProcessSnapshot(pid int) *ProcessSnapshot {
	return collectProcessSnapshotAt("/proc", pid)
}

func collectProcessSnapshotAt(root string, pid int) *ProcessSnapshot {
	snapshot := &ProcessSnapshot{PID: pid, Errors: make(map[string]string)}
	base := fmt.Sprintf("%s/%d/", root, pid)
	read := func(name string, limit int64) string {
		file, err := os.Open(base + name)
		if err != nil {
			snapshot.Errors[name] = procReadError(err)
			return ""
		}
		defer func() { _ = file.Close() }()
		data, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil {
			snapshot.Errors[name] = procReadError(err)
			return ""
		}
		if int64(len(data)) > limit {
			data = data[:limit]
			snapshot.Truncated = true
		}
		return strings.TrimSpace(string(data))
	}
	if raw := read("stat", 2048); raw != "" && !parseProcessStat(raw, snapshot) {
		snapshot.Errors["stat"] = "invalid_numeric_fields"
	}
	if raw := read("status", 8<<10); raw != "" {
		snapshot.Status = parseProcessStatus(raw)
	}
	snapshot.Wchan = read("wchan", 256)
	snapshot.KernelStack = read("stack", 2048)
	snapshot.Syscall = read("syscall", 1024)
	return snapshot
}

func procReadError(err error) string {
	switch {
	case os.IsNotExist(err):
		return "not_found"
	case os.IsPermission(err):
		return "permission_denied"
	default:
		return "read_failed"
	}
}

func parseProcessStat(raw string, snapshot *ProcessSnapshot) bool {
	// comm can contain spaces, parentheses or secrets. Never expose it.
	closing := strings.LastIndexByte(raw, ')')
	if closing < 0 {
		return false
	}
	fields := strings.Fields(raw[closing+1:])
	if len(fields) < 20 || len(fields[0]) != 1 || !strings.ContainsRune("RSDTtZXxKWPI", rune(fields[0][0])) {
		return false
	}
	ppid, errParent := strconv.Atoi(fields[1])
	pgid, errGroup := strconv.Atoi(fields[2])
	session, errSession := strconv.Atoi(fields[3])
	start, errStart := strconv.ParseUint(fields[19], 10, 64)
	if errParent != nil || errGroup != nil || errSession != nil || errStart != nil {
		return false
	}
	snapshot.State, snapshot.PPID, snapshot.PGID, snapshot.Session, snapshot.StartTimeTicks = fields[0], ppid, pgid, session, start
	return true
}

func parseProcessStatus(raw string) map[string]string {
	status := make(map[string]string)
	for line := range strings.SplitSeq(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Pid", "PPid", "Tgid", "NSpid", "TracerPid", "Threads", "Seccomp", "NoNewPrivs",
			"SigPnd", "ShdPnd", "SigBlk", "SigIgn", "SigCgt":
			if value != "" && strings.IndexFunc(value, func(r rune) bool {
				return !strings.ContainsRune("0123456789abcdefABCDEF \t", r)
			}) == -1 {
				status[key] = value
			}
		case "VmRSS", "VmSize":
			fields := strings.Fields(value)
			if len(fields) == 2 && fields[1] == "kB" {
				if _, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
					status[key] = value
				}
			}
		}
	}
	return status
}

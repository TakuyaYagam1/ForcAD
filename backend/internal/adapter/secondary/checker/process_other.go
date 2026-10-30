//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package checker

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

func configureProcess(*exec.Cmd) {}

func killProcess(command *exec.Cmd) error {
	_, err := observeKillProcess(command, false)
	return err
}

func observeTerminateProcess(command *exec.Cmd) (ProcessSignal, error) {
	return observeKillProcess(command, false)
}

func observeKillProcess(command *exec.Cmd, reaped bool) (ProcessSignal, error) {
	result := ProcessSignal{Time: time.Now().UTC(), Signal: "Kill", Target: "process", Outcome: "not_started"}
	if command == nil || command.Process == nil {
		return result, nil
	}
	result.PID = command.Process.Pid
	if reaped {
		result.Outcome = "already_reaped"
		return result, nil
	}
	err := command.Process.Kill()
	if err == nil {
		result.Outcome = "success"
		return result, nil
	}
	result.Error = err.Error()
	if errors.Is(err, os.ErrProcessDone) {
		result.Outcome = "ErrProcessDone"
		return result, nil
	}
	result.Outcome = "error"
	return result, err
}

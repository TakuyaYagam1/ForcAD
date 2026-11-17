//go:build windows

package checker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureProcess(*exec.Cmd) {
}

func killProcess(command *exec.Cmd) error {
	const location = "checker.killProcess"
	_, err := observeKillProcess(command, false)
	if err != nil {
		return fmt.Errorf("%s - kill checker process: %w", location, err)
	}
	return nil
}

func observeTerminateProcess(command *exec.Cmd) (ProcessSignal, error) {
	const location = "checker.observeTerminateProcess"
	result, err := observeKillProcess(command, false)
	if err != nil {
		return result, fmt.Errorf("%s - terminate checker process: %w", location, err)
	}
	return result, nil
}

func observeKillProcess(command *exec.Cmd, reaped bool) (ProcessSignal, error) {
	const location = "checker.observeKillProcess"
	result := ProcessSignal{
		Time:    time.Now().UTC(),
		Signal:  "Kill",
		Target:  "process",
		Outcome: "not_started",
	}
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
	// On Windows Process.Wait releases the handle with statusReleased. A kill
	// racing with its completion therefore returns the bare EINVAL, not ErrProcessDone.
	if errors.Is(err, os.ErrProcessDone) {
		result.Outcome = "ErrProcessDone"
		return result, nil
	}
	if errors.Is(err, syscall.EINVAL) {
		result.Outcome = "EINVAL"
		return result, nil
	}
	result.Outcome = "error"
	return result, fmt.Errorf("%s - kill process: %w", location, err)
}

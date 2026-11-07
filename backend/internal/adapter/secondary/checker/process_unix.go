//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package checker

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func killProcess(command *exec.Cmd) error {
	const location = "checker.killProcess"
	if err := signalProcessGroup(command, syscall.SIGKILL); err != nil {
		return fmt.Errorf("%s - kill checker process group: %w", location, err)
	}
	return nil
}

func signalProcessGroup(command *exec.Cmd, signal syscall.Signal) error {
	const location = "checker.signalProcessGroup"
	_, err := observeProcessGroup(command, signal)
	if err != nil {
		return fmt.Errorf("%s - signal checker process group: %w", location, err)
	}
	return nil
}

func observeTerminateProcess(command *exec.Cmd) (ProcessSignal, error) {
	const location = "checker.observeTerminateProcess"
	result, err := observeProcessGroup(command, syscall.SIGTERM)
	if err != nil {
		return result, fmt.Errorf("%s - terminate checker process group: %w", location, err)
	}
	return result, nil
}

func observeKillProcess(command *exec.Cmd, _ bool) (ProcessSignal, error) {
	const location = "checker.observeKillProcess"
	result, err := observeProcessGroup(command, syscall.SIGKILL)
	if err != nil {
		return result, fmt.Errorf("%s - kill checker process group: %w", location, err)
	}
	return result, nil
}

func observeProcessGroup(command *exec.Cmd, signal syscall.Signal) (ProcessSignal, error) {
	const location = "checker.observeProcessGroup"
	name := "SIGKILL"
	if signal == syscall.SIGTERM {
		name = "SIGTERM"
	}
	result := ProcessSignal{
		Time:    time.Now().UTC(),
		Signal:  name,
		Target:  "process_group",
		Outcome: "not_started",
	}
	if command == nil || command.Process == nil {
		return result, nil
	}
	result.PID, result.PGID = command.Process.Pid, command.Process.Pid
	err := syscall.Kill(-command.Process.Pid, signal)
	if err == nil {
		result.Outcome = "success"
		return result, nil
	}
	result.Error = err.Error()
	if errors.Is(err, syscall.ESRCH) {
		result.Outcome = "ESRCH"
		return result, nil
	}
	result.Outcome = "error"
	return result, fmt.Errorf("%s - signal process group: %w", location, err)
}

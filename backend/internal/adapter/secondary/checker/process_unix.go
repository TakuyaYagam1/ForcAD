//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package checker

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcess(command *exec.Cmd) error {
	return signalProcessGroup(command, syscall.SIGKILL)
}

func signalProcessGroup(command *exec.Cmd, signal syscall.Signal) error {
	_, err := observeProcessGroup(command, signal)
	return err
}

func observeTerminateProcess(command *exec.Cmd) (ProcessSignal, error) {
	return observeProcessGroup(command, syscall.SIGTERM)
}

func observeKillProcess(command *exec.Cmd, _ bool) (ProcessSignal, error) {
	return observeProcessGroup(command, syscall.SIGKILL)
}

func observeProcessGroup(command *exec.Cmd, signal syscall.Signal) (ProcessSignal, error) {
	name := "SIGKILL"
	if signal == syscall.SIGTERM {
		name = "SIGTERM"
	}
	result := ProcessSignal{Time: time.Now().UTC(), Signal: name, Target: "process_group", Outcome: "not_started"}
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
	return result, err
}

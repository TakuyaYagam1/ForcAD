package checker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCompletedCheckerWithRetainedOutputCannotSucceed(t *testing.T) {
	// Keep a writer in this test instead of spawning an orphan. The checker
	// really exits with 101, but EOF cannot arrive until we close the pipe.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, err := newCheckerOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(output.closeReaders)
	t.Cleanup(output.closeWriters)
	//nolint:noctx // The test owns and reaps this process below.
	cmd := exec.Command(executable, "check", "192.0.2.1")
	cmd.Env = setEnvironmentValue(os.Environ(), fixtureModeEnv, "record")
	cmd.Stdout, cmd.Stderr = output.writers[0], output.writers[1]
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = output.writers[1].Close()
	capture := &outputCapture{limit: 4096}
	limit := make(chan struct{}, 1)
	output.start(capture, limit)
	waited := reapChecker(cmd, nil)
	var exit checkerExit
	select {
	case exit = <-waited:
	case <-time.After(5 * time.Second):
		_ = killProcess(cmd)
		<-waited
		t.Fatal("fixture did not exit")
	}
	if exit.exitCode != int(StatusUp) {
		t.Fatalf("fixture exit = %+v", exit)
	}
	// Replay the real process completion after checking that it was independent
	// of output EOF. The buffered result has the same synchronization semantics.
	exited := make(chan checkerExit, 1)
	exited <- exit
	process := startedChecker{command: cmd, output: output, capture: capture, outputLimit: limit, waited: exited}
	started := time.Now()
	actionTimeout := 2 * checkerKillDrainTimeout
	result := Result{Action: ActionPut, Status: StatusCheckFailed, ExitCode: -1}
	wait := waitForChecker(context.Background(), actionTimeout, time.Second, process, &result)
	result, err = finishCheckerResult(Request{Action: ActionPut}, result, wait)
	if !errors.Is(err, errCheckerOutputDrain) || errors.Is(err, errCheckerProcessReap) {
		t.Fatalf("wrong completion failure: result=%+v err=%v", result, err)
	}
	if !result.ProcessReaped || result.OutputComplete || result.TimedOut || result.ExitCode != 101 || result.Status != StatusCheckFailed {
		t.Fatalf("incomplete output accepted or mistaken for a live process: %+v", result)
	}
	if len(result.Stdout) == 0 || time.Since(started) < actionTimeout || time.Since(started) > 2*time.Second {
		t.Fatalf("partial output lost or cleanup unbounded: elapsed=%s result=%+v", time.Since(started), result)
	}
	for _, reader := range output.readers {
		if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("owned reader still open: %v", err)
		}
	}
}

func TestShutdownPreservesFirstCause(t *testing.T) {
	for _, later := range []shutdownReason{shutdownTimeout, shutdownCanceled} {
		t.Run(map[shutdownReason]string{shutdownTimeout: "deadline", shutdownCanceled: "cancellation"}[later], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := Result{Status: StatusCheckFailed, ExitCode: -1}
			state := checkerWait{ctx: ctx, result: &result, contextDone: ctx.Done()}
			defer state.stopTimers()
			state.beginShutdown(shutdownOutputLimit, time.Hour)
			state.beginShutdown(later, time.Hour)
			if later == shutdownCanceled {
				cancel()
				state.waitForEvent(time.Hour)
			}
			finished, err := finishCheckerResult(Request{Action: ActionPut}, result, state.outcome)
			if finished.Status != StatusCheckFailed || !finished.OutputLimited || finished.TimedOut || finished.Canceled || finished.PrivateMessage != checkerOutputLimitMessage {
				t.Fatalf("first shutdown cause overwritten: result=%+v err=%v", finished, err)
			}
			if state.timeout != nil {
				t.Fatal("action deadline still enabled during shutdown")
			}
			if later == shutdownCanceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("later external cancellation was lost: %v", err)
			}
		})
	}
}

func TestOutputLimitIsCheckedAfterProcessAndCopiesFinish(t *testing.T) {
	output, err := newCheckerOutput()
	if err != nil {
		t.Fatal(err)
	}
	defer output.closeReaders()
	defer output.closeWriters()
	capture := &outputCapture{limit: 1}
	capture.append(outputStdout, []byte("too much"))
	output.done <- nil
	output.done <- nil
	exited := make(chan checkerExit, 1)
	exited <- checkerExit{exitCode: 101, reaped: true}
	// No notification is delivered: the final capture check must still reject
	// truncated successful output, regardless of event-selection order.
	process := startedChecker{capture: capture, output: output, waited: exited}
	result := Result{Status: StatusCheckFailed, ExitCode: -1}
	wait := waitForChecker(context.Background(), time.Second, time.Second, process, &result)
	result, err = finishCheckerResult(Request{Action: ActionPut}, result, wait)
	if err != nil || result.Status != StatusCheckFailed || !result.OutputLimited || result.PrivateMessage != checkerOutputLimitMessage {
		t.Fatalf("late output limit ignored: result=%+v err=%v", result, err)
	}
}

func TestCleanupDistinguishesProcessReapingFromOutputDrain(t *testing.T) {
	result := Result{Status: StatusCheckFailed, ExitCode: -1}
	deadline := make(chan time.Time, 1)
	deadline <- time.Now()
	state := checkerWait{result: &result, copiesDone: 2, reap: deadline}
	if !state.waitForEvent(time.Second) || !errors.Is(state.outcome.cleanupErr, errCheckerProcessReap) || errors.Is(state.outcome.cleanupErr, errCheckerOutputDrain) {
		t.Fatalf("wrong cleanup phase: %+v", state.outcome)
	}
}

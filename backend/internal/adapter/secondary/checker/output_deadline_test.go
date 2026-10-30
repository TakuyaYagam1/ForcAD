package checker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCompletedCheckerOutputDrainHonorsContextCancellation(t *testing.T) {
	output, err := newCheckerOutput()
	if err != nil {
		t.Fatal(err)
	}
	defer output.closeReaders()
	defer output.closeWriters()
	capture := &outputCapture{limit: 4096}
	limit := make(chan struct{}, 1)
	// Retain both writers after the leader exits to keep normal output
	// collection pending. Cancellation must not wait for the action deadline.
	output.start(capture, limit)
	exited := make(chan checkerExit)
	process := startedChecker{output: output, capture: capture, outputLimit: limit, waited: exited}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type completion struct {
		result Result
		err    error
	}
	completed := make(chan completion, 1)
	go func() {
		result := Result{Action: ActionPut, Status: StatusCheckFailed, ExitCode: -1}
		wait := waitForChecker(ctx, 5*time.Second, time.Second, process, &result)
		result, err := finishCheckerResult(Request{Action: ActionPut}, result, wait)
		completed <- completion{result: result, err: err}
	}()
	select {
	case exited <- checkerExit{exitCode: 101, reaped: true}:
		// The unbuffered send ensures the exit branch is selected before
		// cancellation, so the case specifically exercises output draining.
	case <-time.After(time.Second):
		t.Fatal("checker exit was not observed")
	}
	cancel()
	select {
	case done := <-completed:
		result := done.result
		if !errors.Is(done.err, context.Canceled) || !errors.Is(done.err, errCheckerOutputDrain) ||
			!result.Canceled || result.TimedOut || !result.Killed || !result.ProcessReaped || result.OutputComplete ||
			result.ExitCode != 101 || result.Status != StatusDown {
			t.Fatalf("cancellation during output drain lost: result=%+v err=%v", result, done.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation waited for the action deadline")
	}
}

func TestCompletedCheckerWaitsForDelayedOutputWithinActionBudget(t *testing.T) {
	output, err := newCheckerOutput()
	if err != nil {
		t.Fatal(err)
	}
	defer output.closeReaders()
	defer output.closeWriters()
	for i, value := range []string{"public data", "private data"} {
		if _, err := output.writers[i].WriteString(value); err != nil {
			t.Fatal(err)
		}
	}
	// No writer remains: the checker has exited and all bytes are available.
	// Only scheduling of the output-copy goroutines is delayed. A cleanup
	// budget after SIGKILL must not shorten the normal action deadline.
	output.closeWriters()
	capture := &outputCapture{limit: 4096}
	limit := make(chan struct{}, 1)
	exited := make(chan checkerExit, 1)
	exited <- checkerExit{exitCode: 101, reaped: true}
	readersStarted := make(chan struct{})
	go func() {
		time.Sleep(2 * checkerKillDrainTimeout)
		output.start(capture, limit)
		close(readersStarted)
	}()
	defer func() { <-readersStarted }()
	process := startedChecker{output: output, capture: capture, outputLimit: limit, waited: exited}
	result := Result{Action: ActionPut, Status: StatusCheckFailed, ExitCode: -1}
	wait := waitForChecker(context.Background(), 5*time.Second, time.Second, process, &result)
	result, err = finishCheckerResult(Request{Action: ActionPut}, result, wait)
	if err != nil || result.Status != StatusUp || !result.ProcessReaped || !result.OutputComplete || result.Killed || result.TimedOut {
		t.Fatalf("healthy completed output rejected: result=%+v err=%v", result, err)
	}
	if result.PublicMessage != "public data" || result.PrivateMessage != "private data" {
		t.Fatalf("completed output was lost: %+v", result)
	}
}

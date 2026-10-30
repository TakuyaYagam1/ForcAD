package checker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

var (
	errCheckerOutputDrain = errors.New("checker output did not drain before cleanup deadline")
	errCheckerProcessReap = errors.New("checker process was not reaped after kill request")
)

type checkerExit struct {
	err      error
	exitCode int
	reaped   bool
}

type startedChecker struct {
	command     *exec.Cmd
	capture     *outputCapture
	output      *checkerOutput
	outputLimit chan struct{}
	waited      <-chan checkerExit
	reaped      <-chan struct{}
}

func startChecker(executable string, args, env []string, outputLimitBytes int) (startedChecker, error) {
	const location = "checker.startChecker"
	//nolint:noctx // Cancellation uses process-group shutdown and independently owned pipes.
	cmd := exec.Command(executable, args...)
	cmd.Env = env
	configureProcess(cmd)
	output, err := newCheckerOutput()
	if err != nil {
		return startedChecker{}, fmt.Errorf("%s - create output pipes: %w", location, err)
	}
	cmd.Stdout, cmd.Stderr = output.writers[0], output.writers[1]
	if err := cmd.Start(); err != nil {
		output.closeWriters()
		output.closeReaders()
		return startedChecker{}, fmt.Errorf("%s - start checker process: %w", location, err)
	}
	output.closeWriters()
	capture := &outputCapture{limit: outputLimitBytes}
	outputLimit := make(chan struct{}, 1)
	output.start(capture, outputLimit)
	reaped := make(chan struct{})
	return startedChecker{command: cmd, capture: capture, output: output, outputLimit: outputLimit, waited: reapChecker(cmd, reaped), reaped: reaped}, nil
}

func reapChecker(cmd *exec.Cmd, reaped chan<- struct{}) <-chan checkerExit {
	waited := make(chan checkerExit, 1)
	go func() {
		err := cmd.Wait()
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		waited <- checkerExit{err: err, exitCode: exitCode, reaped: cmd.ProcessState != nil}
		// Publish the buffered exit first. Seeing completion must also mean
		// the shutdown loop can consume the final exit classification.
		if cmd.ProcessState != nil && reaped != nil {
			close(reaped)
		}
	}()
	return waited
}

type shutdownReason uint8

const (
	shutdownNone shutdownReason = iota
	shutdownTimeout
	shutdownCanceled
	shutdownOutputLimit
	shutdownFailure
)

type checkerWaitResult struct {
	waitErr    error
	contextErr error
	cleanupErr error
	reason     shutdownReason
}

type checkerWait struct {
	ctx     context.Context
	process startedChecker
	result  *Result
	outcome checkerWaitResult

	waited      <-chan checkerExit
	outputDone  <-chan error
	timeout     <-chan time.Time
	contextDone <-chan struct{}
	outputLimit <-chan struct{}
	terminate   <-chan time.Time
	drain       <-chan time.Time
	reap        <-chan time.Time

	terminateTimer  *time.Timer
	drainTimer      *time.Timer
	reapTimer       *time.Timer
	waitDone        bool
	copiesDone      int
	outputFailed    bool
	outputClosed    bool
	killRequested   bool
	diagnosticCount int
	signals         []ProcessSignal
}

func waitForChecker(ctx context.Context, timeout, terminateTimeout time.Duration, process startedChecker, result *Result) checkerWaitResult {
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	state := checkerWait{ctx: ctx, process: process, result: result,
		waited: process.waited, outputDone: process.output.done, timeout: timeoutTimer.C,
		contextDone: ctx.Done(), outputLimit: process.outputLimit}
	defer state.stopTimers()
	defer process.output.closeReaders()
	defer func() {
		result.Stdout, result.Stderr = process.capture.snapshot()
		result.OutputLimited = result.OutputLimited || process.capture.wasLimited()
	}()
	if timeout <= 0 {
		state.beginShutdown(shutdownTimeout, terminateTimeout)
	}
	for {
		if state.waitDone && state.copiesDone == 2 {
			// The process can exit before its final output is copied or the limit
			// notification is selected. Never accept truncated PUT data as success.
			if process.capture.wasLimited() && state.outcome.reason == shutdownNone {
				state.outcome.reason = shutdownOutputLimit
			}
			if state.outcome.reason != shutdownNone && ctx.Err() != nil {
				state.outcome.contextErr = ctx.Err()
			}
			return state.outcome
		}
		if state.waitForEvent(terminateTimeout) {
			return state.outcome
		}
	}
}

func (state *checkerWait) waitForEvent(terminateTimeout time.Duration) bool {
	select {
	case exit := <-state.waited:
		state.acceptExit(exit)
	case err := <-state.outputDone:
		state.acceptOutput(err, terminateTimeout)
	case <-state.timeout:
		state.expireAction(terminateTimeout)
	case <-state.contextDone:
		state.contextDone = nil
		state.outcome.contextErr = state.ctx.Err()
		reason := shutdownCanceled
		if errors.Is(state.outcome.contextErr, context.DeadlineExceeded) {
			reason = shutdownTimeout
		}
		state.beginShutdown(reason, terminateTimeout)
	case <-state.outputLimit:
		state.outputLimit = nil
		state.result.OutputLimited = true
		state.beginShutdown(shutdownOutputLimit, terminateTimeout)
	case <-state.terminate:
		state.terminate = nil
		state.forceKill()
	case <-state.drain:
		state.drain = nil
		state.pollOutput(terminateTimeout)
		if state.copiesDone != 2 {
			state.failOutputDrain()
			state.forceKill()
		}
	case <-state.reap:
		state.reap = nil
		// Prefer completion already queued at the deadline over a false timeout.
		select {
		case exit := <-state.waited:
			state.acceptExit(exit)
		default:
		}
		state.pollOutput(terminateTimeout)
		if !state.waitDone {
			state.outcome.cleanupErr = errors.Join(state.outcome.cleanupErr, errCheckerProcessReap)
			state.reportUnreapedDiagnostic()
		}
		if state.copiesDone != 2 {
			state.failOutputDrain()
		}
		return true
	}
	return false
}

func (state *checkerWait) expireAction(terminateTimeout time.Duration) {
	state.timeout = nil
	state.pollOutput(terminateTimeout)
	if !state.waitDone {
		state.beginShutdown(shutdownTimeout, terminateTimeout)
		return
	}
	// A successful exit is not complete until both pipes reach EOF.
	// Allow output to use the original action budget, while still
	// rejecting a retained writer when that deadline expires.
	if state.copiesDone != 2 {
		state.failOutputDrain()
		state.forceKill()
	}
}

func (state *checkerWait) acceptExit(exit checkerExit) {
	state.waited = nil
	state.waitDone = true
	state.result.ProcessReaped = exit.reaped
	state.result.ExitCode = exit.exitCode
	state.outcome.waitErr = exit.err
	// Keep the action deadline while normal output is draining. The short
	// kill-drain budget applies only after forced process cleanup starts.
	if !exit.reaped {
		state.outcome.cleanupErr = errors.Join(state.outcome.cleanupErr, errCheckerProcessReap)
		if state.outcome.reason == shutdownNone {
			state.outcome.reason = shutdownFailure
		}
	}
	if state.outcome.reason != shutdownNone {
		// The leader may exit on TERM while a child ignores it. Finish group
		// cleanup even though waiting for the leader no longer blocks us.
		state.forceKill()
	}
}

func (state *checkerWait) acceptOutput(err error, terminateTimeout time.Duration) {
	state.copiesDone++
	if err != nil && !state.outputClosed {
		state.outputFailed = true
		state.outcome.cleanupErr = errors.Join(state.outcome.cleanupErr, fmt.Errorf("read checker output: %w", err))
		state.beginShutdown(shutdownFailure, terminateTimeout)
	}
	if state.copiesDone == 2 {
		state.outputDone = nil
		state.result.OutputComplete = !state.outputClosed && !state.outputFailed
	}
}

func (state *checkerWait) pollOutput(terminateTimeout time.Duration) {
	for state.copiesDone != 2 {
		select {
		case err := <-state.outputDone:
			state.acceptOutput(err, terminateTimeout)
		default:
			return
		}
	}
}

func (state *checkerWait) beginShutdown(reason shutdownReason, terminateTimeout time.Duration) {
	if state.outcome.reason != shutdownNone {
		return
	}
	state.outcome.reason = reason
	state.timeout = nil // A later deadline cannot overwrite the initiating cause.
	switch reason {
	case shutdownTimeout:
		state.result.TimedOut = true
	case shutdownCanceled:
		state.result.Canceled = true
	case shutdownOutputLimit:
		state.result.OutputLimited = true
	case shutdownNone, shutdownFailure:
	}
	if state.waitDone {
		state.forceKill()
		return
	}
	signal, err := observeTerminateProcess(state.process.command)
	state.signals = append(state.signals, signal)
	if err != nil {
		state.outcome.cleanupErr = errors.Join(state.outcome.cleanupErr, fmt.Errorf("terminate checker process group: %w", err))
	}
	state.terminateTimer = time.NewTimer(terminateTimeout)
	state.terminate = state.terminateTimer.C
	// Preserve the wait stack even if both diagnostic slots later block in proc.
	state.reportDiagnostic("shutdown", reason == shutdownTimeout)
}

func (state *checkerWait) forceKill() {
	if state.killRequested {
		return
	}
	state.killRequested = true
	state.result.Killed = true
	if state.terminateTimer != nil {
		state.terminateTimer.Stop()
		state.terminate = nil
	}
	signal, err := observeKillProcess(state.process.command, state.result.ProcessReaped)
	state.signals = append(state.signals, signal)
	if err != nil {
		state.outcome.cleanupErr = errors.Join(state.outcome.cleanupErr, fmt.Errorf("kill checker process group: %w", err))
	}
	state.startDrainTimer()
	// Preserve the former total cleanup budget: 250ms drain + 500ms reap.
	state.reapTimer = time.NewTimer(checkerKillDrainTimeout + checkerProcessReapTimeout)
	state.reap = state.reapTimer.C
	state.reportDiagnostic("kill", false)
}

func (state *checkerWait) startDrainTimer() {
	if state.drainTimer == nil {
		state.drainTimer = time.NewTimer(checkerKillDrainTimeout)
		state.drain = state.drainTimer.C
	}
}

func (state *checkerWait) failOutputDrain() {
	if !state.outputClosed {
		if state.outcome.reason == shutdownNone {
			state.reportDiagnostic("drain", true)
		}
		state.outcome.cleanupErr = errors.Join(state.outcome.cleanupErr, errCheckerOutputDrain)
		state.outputClosed = true
		state.result.OutputComplete = false
		state.process.output.closeReaders()
	}
	if state.outcome.reason == shutdownNone {
		state.outcome.reason = shutdownFailure
	}
}

func (state *checkerWait) stopTimers() {
	for _, timer := range []*time.Timer{state.terminateTimer, state.drainTimer, state.reapTimer} {
		if timer != nil {
			timer.Stop()
		}
	}
}

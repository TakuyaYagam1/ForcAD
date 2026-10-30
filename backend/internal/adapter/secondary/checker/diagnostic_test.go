package checker

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticMetadataSurvivesBlockedProcRead(t *testing.T) {
	events := make(chan ProcessDiagnostic, 2)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	done := make(chan struct{})
	requested := time.Now().UTC()
	go func() {
		defer close(done)
		collectAndReportDiagnostic(ProcessDiagnostic{PID: 123, Phase: "shutdown", RequestedAt: requested, TimedOut: true,
			Signals: []ProcessSignal{{Signal: "SIGTERM", Outcome: "success"}}}, true,
			func(event ProcessDiagnostic) { events <- event }, func(pid int) *ProcessSnapshot {
				<-release
				return &ProcessSnapshot{PID: pid, State: "D"}
			})
	}()
	initial := receiveDiagnostic(t, events)
	if initial.SnapshotComplete || initial.Proc != nil || initial.Goroutines == "" || len(initial.Goroutines) > diagnosticStackBytes ||
		!initial.TimedOut || initial.Signals[0].Outcome != "success" || initial.CollectedAt.Before(requested) {
		t.Fatalf("initial evidence missing before proc read: %+v", initial)
	}
	unblock()
	complete := receiveDiagnostic(t, events)
	if !complete.SnapshotComplete || complete.Proc.State != "D" || complete.FinishedAt.Before(complete.CollectedAt) {
		t.Fatalf("enriched evidence missing: %+v", complete)
	}
	<-done
}

func TestDiagnosticObserverCannotBlockShutdownOrChangeFirstCause(t *testing.T) {
	events := make(chan ProcessDiagnostic, 4)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	ctx, cancel := context.WithCancel(WithDiagnosticHandler(context.Background(), func(event ProcessDiagnostic) {
		events <- event
		if !event.SnapshotComplete {
			<-release
		}
	}))
	defer cancel()
	result := Result{OutputLimited: true, ExitCode: -1}
	state := checkerWait{ctx: ctx, result: &result, contextDone: ctx.Done(), outcome: checkerWaitResult{reason: shutdownOutputLimit},
		process: startedChecker{command: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}, Args: []string{"FLAG_SECRET_ARG"}, Env: []string{"FLAG_SECRET_ENV"}}},
		signals: []ProcessSignal{{Signal: "SIGTERM", Outcome: "success"}}}
	if !state.reportDiagnostic("shutdown", false) || !state.reportDiagnostic("kill", false) {
		t.Fatal("initial diagnostic slots unavailable")
	}
	// Both observers are blocked; a third event must be dropped, not delayed.
	if state.reportDiagnostic("unreaped", true) {
		t.Fatal("diagnostic helper cap exceeded")
	}
	state.signals[0].Outcome = "changed after dispatch"
	state.beginShutdown(shutdownTimeout, time.Second)
	cancel()
	state.waitForEvent(time.Second)
	if state.outcome.reason != shutdownOutputLimit || result.TimedOut || result.Canceled || !result.OutputLimited {
		t.Fatalf("diagnostics changed first shutdown cause: %+v %+v", result, state.outcome)
	}
	for range 2 {
		event := receiveDiagnostic(t, events)
		encoded, err := json.Marshal(event)
		if err != nil || strings.Contains(string(encoded), "FLAG_SECRET") || event.Signals[0].Outcome != "success" {
			t.Fatalf("diagnostic exposed command data or shared mutable state: %s, %v", encoded, err)
		}
	}
	unblock()
	for range 2 {
		if !receiveDiagnostic(t, events).SnapshotComplete {
			t.Fatal("missing final snapshot")
		}
	}
	deadline := time.Now().Add(time.Second)
	for len(processDiagnosticSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(processDiagnosticSlots) != 0 || state.reportDiagnostic("extra", false) {
		t.Fatal("diagnostic helpers leaked or per-action cap exceeded")
	}
}

func receiveDiagnostic(t *testing.T, events <-chan ProcessDiagnostic) ProcessDiagnostic {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic event missing")
		return ProcessDiagnostic{}
	}
}

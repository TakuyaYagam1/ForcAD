package checker

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"runtime"
	"time"
)

const diagnosticStackBytes = 128 << 10

// A blocked proc read or observer occupies a slot, never the shutdown loop.
var processDiagnosticSlots = make(chan struct{}, 2)

type diagnosticHandlerKey struct{}

// WithDiagnosticHandler installs an asynchronous observer for exceptional
// shutdown. No command arguments, environment or checker output are reported.
func WithDiagnosticHandler(ctx context.Context, handler func(ProcessDiagnostic)) context.Context {
	return context.WithValue(ctx, diagnosticHandlerKey{}, handler)
}

// Proc is sampled asynchronously. Its PID may have been reused: compare the
// sampled parent/start time and timestamps, not just the requested PID.
type ProcessDiagnostic struct {
	Phase            string           `json:"phase"`
	PID              int              `json:"pid"`
	RequestedAt      time.Time        `json:"requested_at"`
	CollectedAt      time.Time        `json:"collected_at"`
	FinishedAt       time.Time        `json:"finished_at"`
	ProcessReaped    bool             `json:"process_reaped"`
	OutputComplete   bool             `json:"output_complete"`
	TimedOut         bool             `json:"timed_out"`
	Canceled         bool             `json:"canceled"`
	OutputLimited    bool             `json:"output_limited"`
	ExitCode         int              `json:"exit_code"`
	Signals          []ProcessSignal  `json:"signals,omitempty"`
	Proc             *ProcessSnapshot `json:"proc,omitempty"`
	Goroutines       string           `json:"goroutines,omitempty"`
	Truncated        bool             `json:"truncated,omitempty"`
	SnapshotComplete bool             `json:"snapshot_complete"`
}

// MarshalJSON preserves nested diagnostics in logger adapters that stringify
// arbitrary structs but support JSON marshalers.
func (diagnostic ProcessDiagnostic) MarshalJSON() ([]byte, error) {
	type diagnosticJSON ProcessDiagnostic
	return jsonv2.Marshal(diagnosticJSON(diagnostic))
}

type ProcessSignal struct {
	Time    time.Time `json:"time"`
	Signal  string    `json:"signal"`
	Target  string    `json:"target"`
	PID     int       `json:"pid"`
	PGID    int       `json:"pgid,omitempty"`
	Outcome string    `json:"outcome"`
	Error   string    `json:"error,omitempty"`
}

type ProcessSnapshot struct {
	PID            int               `json:"pid"`
	PPID           int               `json:"ppid"`
	PGID           int               `json:"pgid"`
	Session        int               `json:"session"`
	State          string            `json:"state"`
	StartTimeTicks uint64            `json:"start_time_ticks"`
	Status         map[string]string `json:"status,omitempty"`
	Wchan          string            `json:"wchan,omitempty"`
	KernelStack    string            `json:"kernel_stack,omitempty"`
	Syscall        string            `json:"syscall,omitempty"`
	Errors         map[string]string `json:"errors,omitempty"`
	Truncated      bool              `json:"truncated,omitempty"`
}

// Called only by the single shutdown-state goroutine. Events own their data;
// neither asynchronous collection nor the observer can mutate runner state.
func (state *checkerWait) reportDiagnostic(phase string, stack bool) bool {
	if state.ctx == nil || state.process.command == nil || state.process.command.Process == nil {
		return false
	}
	handler, _ := state.ctx.Value(diagnosticHandlerKey{}).(func(ProcessDiagnostic))
	if handler == nil || state.diagnosticCount >= 3 {
		return false
	}
	state.diagnosticCount++
	event := ProcessDiagnostic{Phase: phase, PID: state.process.command.Process.Pid, RequestedAt: time.Now().UTC(),
		ProcessReaped: state.result.ProcessReaped, OutputComplete: state.result.OutputComplete,
		TimedOut: state.result.TimedOut, Canceled: state.result.Canceled, OutputLimited: state.result.OutputLimited,
		ExitCode: state.result.ExitCode, Signals: append([]ProcessSignal(nil), state.signals...)}
	select {
	case processDiagnosticSlots <- struct{}{}:
	default:
		return false
	}
	go func() {
		defer func() { <-processDiagnosticSlots }()
		collectAndReportDiagnostic(event, stack, handler, collectProcessSnapshot)
	}()
	return true
}

func collectAndReportDiagnostic(event ProcessDiagnostic, stack bool, handler func(ProcessDiagnostic), collect func(int) *ProcessSnapshot) {
	event.CollectedAt = time.Now().UTC()
	// Capture the Go stack first: a proc read itself may block in the kernel.
	if stack {
		buffer := make([]byte, diagnosticStackBytes)
		n := runtime.Stack(buffer, true)
		event.Goroutines = string(buffer[:n])
		event.Truncated = n == len(buffer)
	}
	handler(event) // Preserve metadata/stack even if a proc read blocks.
	event.Proc = collect(event.PID)
	event.FinishedAt = time.Now().UTC()
	event.SnapshotComplete = true
	handler(event)
}

func (state *checkerWait) reportUnreapedDiagnostic() {
	dispatched := state.reportDiagnostic("unreaped", true)
	if state.process.command != nil && state.process.command.Process != nil {
		state.outcome.cleanupErr = fmt.Errorf("pid=%d phase=unreaped diagnostic_dispatched=%t: %w", state.process.command.Process.Pid, dispatched, state.outcome.cleanupErr)
	}
}

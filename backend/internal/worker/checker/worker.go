package checker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"uuid"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

const (
	defaultLeaseDuration  = 30 * time.Second
	defaultRenewInterval  = 10 * time.Second
	defaultRecoveryGrace  = 30 * time.Second
	defaultWorkflowLimit  = 30 * time.Minute
	defaultActionLimit    = 16
	maxWorkflowActions    = 1024
	maxCheckerRound       = int64(1<<31 - 1)
	stateCheckTimeout     = 3 * time.Second
	checkFailedStatus     = int32(processchecker.StatusCheckFailed)
	workflowTimeoutStatus = checkFailedStatus
	flagAlphabet          = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var (
	ErrLeaseLost               = errors.New("checker job lease lost")
	ErrNoActions               = errors.New("checker workflow has no actions")
	ErrWorkflowDeadline        = errors.New("checker workflow deadline exceeded")
	ErrWorkflowRecoveryExpired = errors.New("checker workflow recovery grace expired")
	ErrWorkerUnreaped          = errors.New("worker has an unreaped checker process; restart required")
)

// Runner executes one external checker protocol action.
type Runner interface {
	Run(ctx context.Context, request processchecker.Request) (processchecker.Result, error)
}

// Repository is the durable job and team surface used by a checker worker.
type Repository interface {
	outbound.JobRepository
	IsActiveTeam(ctx context.Context, teamID uuid.UUID) (bool, error)
	SetDeadline(ctx context.Context, jobID uuid.UUID, ownerToken string, now, deadline time.Time) (bool, error)
}

// FlagStore persists PUT output and loads a flag from a selected round.
type FlagStore interface {
	Create(ctx context.Context, flag domain.Flag, owner *domain.CheckerJobOwner) (domain.Flag, error)
	FindRandomRound(ctx context.Context, teamID, taskID uuid.UUID, round int32) (domain.Flag, error)
}

// LifecycleReader supplies the current flag lifetime for GET selection.
type LifecycleReader interface {
	Lifecycle(ctx context.Context) (domain.Lifecycle, error)
}

// Observer records checker activity without affecting job completion.
type Observer interface {
	WorkflowStarted()
	WorkflowEnded()
	ActionStarted()
	ActionEnded()
	ObserveCompleted(service, team string, result domain.CheckerResult, timedOut bool)
}

// Options controls durable claim renewal, bounded recovery and workflow runtime.
type Options struct {
	LeaseDuration       time.Duration
	RenewInterval       time.Duration
	RecoveryGrace       time.Duration
	MaxWorkflowDuration time.Duration
	WorkConcurrency     int
	Observer            Observer
	// OnDegraded stops readiness/intake when an unreaped checker is detected.
	// The callback must return immediately.
	OnDegraded func()
}

// Worker executes one complete durable checker workflow per queue delivery.
type Worker struct {
	repo      Repository
	flags     FlagStore
	lifecycle LifecycleReader
	runner    Runner
	logger    *slog.Logger
	options   Options
	observer  Observer
	actions   chan struct{}
	degraded  chan struct{}
	failures  chan error
	degrade   sync.Once
	processMu sync.Mutex
	unreaped  map[uint64]time.Time
	processes uint64
}

// New constructs a worker. The action semaphore belongs to this instance and
// therefore limits concurrent checker processes across all handled jobs.
func New(repo Repository, flags FlagStore, lifecycle LifecycleReader, runner Runner, options Options, logger *slog.Logger) *Worker {
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultLeaseDuration
	}
	if options.RecoveryGrace <= 0 {
		options.RecoveryGrace = defaultRecoveryGrace
	}
	if options.MaxWorkflowDuration <= 0 {
		options.MaxWorkflowDuration = defaultWorkflowLimit
	}
	if options.RenewInterval <= 0 {
		options.RenewInterval = defaultRenewInterval
	}
	if options.RenewInterval >= options.LeaseDuration {
		options.RenewInterval = options.LeaseDuration / 3
	}
	if options.RenewInterval <= 0 {
		options.RenewInterval = time.Second
	}
	if options.WorkConcurrency <= 0 {
		options.WorkConcurrency = defaultActionLimit
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		repo: repo, flags: flags, lifecycle: lifecycle, runner: runner, logger: logger,
		options: options, actions: make(chan struct{}, options.WorkConcurrency),
		observer: options.Observer,
		degraded: make(chan struct{}), failures: make(chan error, 1), unreaped: make(map[uint64]time.Time),
	}
}

// Handle claims and runs the whole workflow for a stable queue job ID. It
// returns nil only after the result is durable or the job is known to be closed.
func (w *Worker) Handle(ctx context.Context, jobID uuid.UUID) error {
	const location = "checker.Worker.Handle"
	if ctx == nil {
		return fmt.Errorf("%s - checker worker context must not be nil", location)
	}
	if jobID == uuid.Nil() {
		return fmt.Errorf("%s - checker job ID must not be nil", location)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - accept checker job: %w", location, err)
	}
	if w.Degraded() {
		return ErrWorkerUnreaped
	}

	claim, claimed, err := w.claimJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("%s - claim checker job: %w", location, err)
	}
	if !claimed {
		return nil
	}
	if w.Degraded() {
		return ErrWorkerUnreaped
	}
	if w.observer != nil {
		w.observer.WorkflowStarted()
		defer w.observer.WorkflowEnded()
	}

	workflowDeadline := claim.claimedAt.Add(w.options.MaxWorkflowDuration)
	recoveryDeadline := workflowDeadline.Add(w.options.RecoveryGrace)
	if claim.job.ExpiresAt != nil {
		// A queue delay must not consume the service's action timeout or turn
		// an incomplete observation into a DOWN verdict.
		if cutoff := claim.job.ExpiresAt.Add(-time.Second); cutoff.Before(workflowDeadline) {
			workflowDeadline = cutoff
		}
		recoveryDeadline = *claim.job.ExpiresAt
	}
	// Recovery may outlive execution, but must also have a hard deadline.
	// In particular, an in-flight lease RPC must not keep the job alive forever.
	recoveryCtx, stopRecovery := context.WithDeadlineCause(ctx, recoveryDeadline, ErrWorkflowRecoveryExpired)
	defer stopRecovery()
	workCtx, cancel := context.WithCancelCause(recoveryCtx)
	defer cancel(context.Canceled)
	guard := &leaseGuard{
		worker: w, jobID: jobID, ownerToken: claim.ownerToken, ctx: workCtx,
		cancelFn: cancel, done: make(chan struct{}), round: claim.job.Round, workflowDeadline: workflowDeadline,
		recoveryDeadline: recoveryDeadline,
	}
	//nolint:contextcheck // The guard owns workCtx, the recovery-bounded child created above.
	go guard.run()
	workflowCtx, cancelWorkflow := context.WithDeadlineCause(workCtx, workflowDeadline, ErrWorkflowDeadline)
	result, executeErr := w.execute(workflowCtx, guard, claim.job, claim.ownerToken)
	cancelWorkflow()
	if err := w.finish(ctx, guard, claim.job, result, executeErr); err != nil {
		return fmt.Errorf("%s - finish checker job: %w", location, err)
	}
	return nil
}

package checker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

// A lost database response must honor the context supplied by the worker.
type stalledRepository struct {
	*fakeRepository

	operation string
	entered   chan struct{}
	once      sync.Once
}

func (r *stalledRepository) stall(ctx context.Context) (bool, error) {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	return false, ctx.Err()
}

func (r *stalledRepository) SetDeadline(ctx context.Context, id uuid.UUID, owner string, at, deadline time.Time) (bool, error) {
	if r.operation == "deadline" {
		return r.stall(ctx)
	}
	return r.fakeRepository.SetDeadline(ctx, id, owner, at, deadline)
}

func (r *stalledRepository) Renew(ctx context.Context, id uuid.UUID, owner string, at, deadline time.Time) (bool, error) {
	if r.operation == "lease" {
		return r.stall(ctx)
	}
	return r.fakeRepository.Renew(ctx, id, owner, at, deadline)
}

func (r *stalledRepository) Complete(ctx context.Context, id uuid.UUID, owner string, at time.Time, result domain.CheckerResult) (bool, error) {
	if r.operation == "complete" {
		return r.stall(ctx)
	}
	return r.fakeRepository.Complete(ctx, id, owner, at, result)
}

func TestStalledRepositoryCannotOutliveWorkflowRecovery(t *testing.T) {
	for _, operation := range []string{"deadline", "lease", "complete"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				task := defaultTask()
				task.Puts = 1
				repo := &stalledRepository{fakeRepository: newFakeRepository(payloadJSON(t, "puts_only", task), domain.JobActionPutsOnly), operation: operation, entered: make(chan struct{})}
				future := time.Now().Add(time.Hour)
				repo.setState(testJobID, domain.CheckerJobState{DeadlineAt: &future})
				runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
					return success(req.Action), nil
				}}
				worker := New(repo, &fakeFlagStore{}, fakeLifecycle{lifetime: 3}, runner, Options{
					LeaseDuration: time.Second, RenewInterval: 500 * time.Millisecond,
					MaxWorkflowDuration: 100 * time.Millisecond, RecoveryGrace: 80 * time.Millisecond, WorkConcurrency: 1,
				}, nil)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- worker.Handle(ctx, testJobID) }()
				<-repo.entered
				synctest.Sleep(200 * time.Millisecond)
				select {
				case err := <-done:
					if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrWorkflowRecoveryExpired) {
						t.Errorf("Handle() = %v, want a bounded database/deadline failure", err)
					}
				default:
					cancel()
					<-done
					t.Error("database operation kept Handle alive beyond workflow and recovery deadlines")
				}
				_, accounted, _, _, _ := repo.snapshot()
				if accounted != 0 {
					t.Errorf("stalled operation incorrectly accounted %d results", accounted)
				}
			})
		})
	}
}

func TestLeaseIOLeavesCheckerLogsAccessible(t *testing.T) {
	repo := &stalledRepository{fakeRepository: newFakeRepository(nil, domain.JobActionCheckOnly), operation: "deadline", entered: make(chan struct{})}
	worker := New(repo, &fakeFlagStore{}, fakeLifecycle{lifetime: 3}, &fakeRunner{}, Options{}, nil)
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(context.Canceled)
	guard := &leaseGuard{worker: worker, jobID: testJobID, ownerToken: "owner", ctx: ctx, cancelFn: cancel,
		workflowDeadline: time.Now().Add(time.Minute), recoveryDeadline: time.Now().Add(2 * time.Minute)}
	renewed := make(chan error, 1)
	go func() { renewed <- guard.renewNow() }()
	<-repo.entered
	logged := make(chan struct{})
	go func() { guard.appendCheckerLog(domain.CheckerLog{Action: "put"}); close(logged) }()
	select {
	case <-logged:
	case <-time.After(100 * time.Millisecond):
		t.Error("lease database I/O blocked checker state/log mutex")
	}
	cancel(context.Canceled)
	<-renewed
	<-logged
	if logs := guard.completedCheckerLogs(); len(logs) != 1 {
		t.Fatalf("completed logs = %d, want 1", len(logs))
	}
}

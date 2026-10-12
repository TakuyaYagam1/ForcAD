package game

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type runtimeCall struct {
	status domain.RuntimeStatus
	err    error
}

type runtimeDeadline struct {
	deadline time.Time
	ok       bool
}

type runtimeGateRepository struct {
	outbound.GameRepository

	calls       atomic.Int64
	started     [2]chan struct{}
	release     [2]chan struct{}
	releaseOnce [2]sync.Once
	deadlines   chan runtimeDeadline
}

func newRuntimeGateRepository() *runtimeGateRepository {
	return &runtimeGateRepository{
		started: [2]chan struct{}{make(chan struct{}), make(chan struct{})},
		release: [2]chan struct{}{make(chan struct{}), make(chan struct{})},
	}
}

func (r *runtimeGateRepository) unblock(call int) {
	r.releaseOnce[call-1].Do(func() { close(r.release[call-1]) })
}

func (r *runtimeGateRepository) Lifecycle(ctx context.Context) (domain.Lifecycle, error) {
	call := r.calls.Add(1)
	if r.deadlines != nil {
		deadline, ok := ctx.Deadline()
		select {
		case r.deadlines <- runtimeDeadline{deadline: deadline, ok: ok}:
		default:
		}
	}
	if call > int64(len(r.started)) {
		return domain.Lifecycle{Config: domain.GameConfig{RealRound: int32(call)}}, nil
	}
	index := int(call - 1)
	close(r.started[index])
	select {
	case <-r.release[index]:
		return domain.Lifecycle{Config: domain.GameConfig{RealRound: int32(call)}}, nil
	case <-ctx.Done():
		return domain.Lifecycle{}, ctx.Err()
	}
}

type runtimeObservedContext struct {
	context.Context

	ready chan struct{}
	once  sync.Once
}

func (c *runtimeObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.ready) })
	return c.Context.Done()
}

func startRuntimeCall(ctx context.Context, service *Service) <-chan runtimeCall {
	result := make(chan runtimeCall, 1)
	go func() {
		status, err := service.Runtime(ctx)
		result <- runtimeCall{status: status, err: err}
	}()
	return result
}

func awaitRuntimeSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func awaitRuntimeResult(t *testing.T, result <-chan runtimeCall, name string) runtimeCall {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		return runtimeCall{}
	}
}

func TestRuntimeSingleflightWaiterCancellationIsIndependent(t *testing.T) {
	repo := newRuntimeGateRepository()
	repo.deadlines = make(chan runtimeDeadline, 1)
	service := New(repo, nil, nil, slog.New(slog.DiscardHandler))
	defer repo.unblock(1)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	defer cancelLeader()
	leader := startRuntimeCall(leaderCtx, service)
	awaitRuntimeSignal(t, repo.started[0], "shared lifecycle load")
	deadline := <-repo.deadlines
	if !deadline.ok {
		t.Fatal("shared load has no deadline")
	}
	remaining := time.Until(deadline.deadline)
	if remaining <= 0 || remaining > runtimeLifecycleTimeout {
		t.Fatalf("shared load deadline remaining = %s, want (0, %s]", remaining, runtimeLifecycleTimeout)
	}

	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	defer cancelWaiter()
	waiterObserved := &runtimeObservedContext{Context: waiterCtx, ready: make(chan struct{})}
	waiter := startRuntimeCall(waiterObserved, service)
	awaitRuntimeSignal(t, waiterObserved.ready, "cancelable waiter registration")

	survivorObserved := &runtimeObservedContext{Context: context.Background(), ready: make(chan struct{})}
	survivor := startRuntimeCall(survivorObserved, service)
	awaitRuntimeSignal(t, survivorObserved.ready, "surviving waiter registration")

	cancelLeader()
	cancelWaiter()
	if got := awaitRuntimeResult(t, leader, "canceled leader"); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("leader error = %v, want context.Canceled", got.err)
	}
	if got := awaitRuntimeResult(t, waiter, "canceled waiter"); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("waiter error = %v, want context.Canceled", got.err)
	}
	if got := repo.calls.Load(); got != 1 {
		t.Fatalf("Lifecycle calls before releasing shared load = %d, want 1", got)
	}

	repo.unblock(1)
	if got := awaitRuntimeResult(t, survivor, "surviving waiter result"); got.err != nil {
		t.Fatalf("surviving waiter error = %v", got.err)
	}
	if got := repo.calls.Load(); got != 1 {
		t.Fatalf("Lifecycle calls = %d, want 1", got)
	}
}

func TestRuntimeChangedStartsFreshLoadBeforeOldLoadFinishes(t *testing.T) {
	repo := newRuntimeGateRepository()
	service := New(repo, nil, nil, slog.New(slog.DiscardHandler))
	defer repo.unblock(1)
	defer repo.unblock(2)

	oldResult := startRuntimeCall(context.Background(), service)
	awaitRuntimeSignal(t, repo.started[0], "pre-change lifecycle load")
	service.Changed()

	newResult := startRuntimeCall(context.Background(), service)
	awaitRuntimeSignal(t, repo.started[1], "post-change lifecycle load")
	if got := repo.calls.Load(); got != 2 {
		t.Fatalf("Lifecycle calls while old load remains blocked = %d, want 2", got)
	}

	repo.unblock(2)
	if got := awaitRuntimeResult(t, newResult, "post-change result"); got.err != nil || got.status.Round != 2 {
		t.Fatalf("post-change result = %+v, want round 2 without error", got)
	}
	repo.unblock(1)
	if got := awaitRuntimeResult(t, oldResult, "pre-change result"); got.err != nil || got.status.Round != 1 {
		t.Fatalf("pre-change result = %+v, want round 1 without error", got)
	}
}

func TestRuntimeCloseCancelsForgottenLoadsAndRejectsNewLoads(t *testing.T) {
	repo := newRuntimeGateRepository()
	service := New(repo, nil, nil, slog.New(slog.DiscardHandler))
	defer repo.unblock(1)
	defer repo.unblock(2)

	oldResult := startRuntimeCall(context.Background(), service)
	awaitRuntimeSignal(t, repo.started[0], "pre-change lifecycle load")
	service.Changed()
	newResult := startRuntimeCall(context.Background(), service)
	awaitRuntimeSignal(t, repo.started[1], "post-change lifecycle load")

	service.Close()
	service.Close()
	if got := awaitRuntimeResult(t, oldResult, "forgotten load cancellation"); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("forgotten load error = %v, want context.Canceled", got.err)
	}
	if got := awaitRuntimeResult(t, newResult, "current load cancellation"); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("current load error = %v, want context.Canceled", got.err)
	}

	if _, err := service.Runtime(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Runtime() after Close error = %v, want context.Canceled", err)
	}
	if calls := repo.calls.Load(); calls != 2 {
		t.Fatalf("Lifecycle calls after Close = %d, want no new read", calls)
	}
}

type runtimeRetryRepository struct {
	outbound.GameRepository

	calls atomic.Int64
	err   error
}

func (r *runtimeRetryRepository) Lifecycle(context.Context) (domain.Lifecycle, error) {
	if r.calls.Add(1) == 1 {
		return domain.Lifecycle{}, r.err
	}
	return domain.Lifecycle{Config: domain.GameConfig{RealRound: 7}}, nil
}

func TestRuntimeDoesNotCacheErrors(t *testing.T) {
	wantErr := errors.New("temporary lifecycle failure")
	repo := &runtimeRetryRepository{err: wantErr}
	service := New(repo, nil, nil, slog.New(slog.DiscardHandler))

	if _, err := service.Runtime(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("first Runtime() error = %v, want wrapped temporary error", err)
	}
	got, err := service.Runtime(context.Background())
	if err != nil || got.Round != 7 {
		t.Fatalf("second Runtime() = %+v, %v, want round 7", got, err)
	}
	if calls := repo.calls.Load(); calls != 2 {
		t.Fatalf("Lifecycle calls = %d, want 2 after retry", calls)
	}
}

type runtimeTimeoutRepository struct {
	outbound.GameRepository

	started  chan struct{}
	deadline chan runtimeDeadline
	finished chan error
	calls    atomic.Int64
}

func (r *runtimeTimeoutRepository) Lifecycle(ctx context.Context) (domain.Lifecycle, error) {
	r.calls.Add(1)
	deadline, ok := ctx.Deadline()
	r.deadline <- runtimeDeadline{deadline: deadline, ok: ok}
	close(r.started)
	<-ctx.Done()
	err := ctx.Err()
	r.finished <- err
	return domain.Lifecycle{}, err
}

func TestRuntimeSharedLoadHasBoundedTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &runtimeTimeoutRepository{
			started:  make(chan struct{}),
			deadline: make(chan runtimeDeadline, 1),
			finished: make(chan error, 1),
		}
		service := New(repo, nil, nil, slog.New(slog.DiscardHandler))
		result := startRuntimeCall(t.Context(), service)

		<-repo.started
		deadline := <-repo.deadline
		if !deadline.ok || time.Until(deadline.deadline) != runtimeLifecycleTimeout {
			t.Fatalf("shared load deadline = %+v, want exactly %s from start", deadline, runtimeLifecycleTimeout)
		}

		time.Sleep(runtimeLifecycleTimeout)
		synctest.Wait()
		if err := <-repo.finished; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shared load error = %v, want context.DeadlineExceeded", err)
		}
		if got := <-result; !errors.Is(got.err, context.DeadlineExceeded) {
			t.Fatalf("Runtime() error = %v, want context.DeadlineExceeded", got.err)
		}
		if calls := repo.calls.Load(); calls != 1 {
			t.Fatalf("Lifecycle calls = %d, want 1", calls)
		}
	})
}

package usecase

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type lifecycleCall struct {
	life domain.Lifecycle
	err  error
}

type lifecycleGateRepository struct {
	*runtimeGateRepository

	taskCalls atomic.Int64
}

func (r *lifecycleGateRepository) Tasks(context.Context, bool) ([]domain.Task, error) {
	r.taskCalls.Add(1)
	return nil, nil
}

func newLifecycleGateGame(t *testing.T) (*Game, *lifecycleGateRepository) {
	t.Helper()
	repo := &lifecycleGateRepository{
		runtimeGateRepository: newRuntimeGateRepository(),
	}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(service.Close)
	t.Cleanup(func() {
		repo.unblock(1)
		repo.unblock(2)
	})
	return service, repo
}

func startLifecycleCall(ctx context.Context, service *Game) <-chan lifecycleCall {
	result := make(chan lifecycleCall, 1)
	go func() {
		life, err := service.Lifecycle(ctx)
		result <- lifecycleCall{
			life: life,
			err:  err,
		}
	}()
	return result
}

func awaitLifecycleResult(t *testing.T, result <-chan lifecycleCall) lifecycleCall {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle caller did not finish")
		return lifecycleCall{}
	}
}

func awaitLifecycleAttackResult(t *testing.T, result <-chan attackFlightResult) attackFlightResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("attack-data caller did not finish")
		return attackFlightResult{}
	}
}

func TestLifecycleCoalescesMixedCallersWithIndependentCancellation(t *testing.T) {
	service, repo := newLifecycleGateGame(t)
	leaderCtx, cancelLeader := context.WithCancel(t.Context())
	defer cancelLeader()
	leader := startLifecycleCall(leaderCtx, service)
	awaitRuntimeSignal(t, repo.started[0], "shared metadata query")
	waiterCtx, cancelWaiter := context.WithCancel(t.Context())
	defer cancelWaiter()
	waiterObserved := &runtimeObservedContext{
		Context: waiterCtx,
		ready:   make(chan struct{}),
	}
	waiter := startAttackFlight(waiterObserved, service, nil)
	awaitRuntimeSignal(t, waiterObserved.ready, "attack-data lifecycle waiter")
	survivorObserved := &runtimeObservedContext{
		Context: t.Context(),
		ready:   make(chan struct{}),
	}
	survivor := startRuntimeCall(survivorObserved, service)
	awaitRuntimeSignal(t, survivorObserved.ready, "runtime lifecycle waiter")
	if got := repo.calls.Load(); got != 1 {
		t.Fatalf("mixed caller queries=%d, want one", got)
	}
	cancelLeader()
	cancelWaiter()
	if got := awaitLifecycleResult(t, leader); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("leader cancellation lost: %v", got.err)
	}
	if got := awaitLifecycleAttackResult(t, waiter); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("attack-data waiter cancellation lost: %v", got.err)
	}
	repo.unblock(1)
	if got := awaitRuntimeResult(t, survivor, "surviving runtime caller"); got.err != nil || got.status.Round != 1 {
		t.Fatalf("surviving caller failed: %+v", got)
	}
	if repo.calls.Load() != 1 || repo.taskCalls.Load() != 0 {
		t.Fatalf("canceled caller continued work: lifecycle=%d tasks=%d", repo.calls.Load(), repo.taskCalls.Load())
	}
}

func TestLifecycleMixedSuccessfulCallersShareRead(t *testing.T) {
	service, repo := newLifecycleGateGame(t)
	leader := startLifecycleCall(t.Context(), service)
	awaitRuntimeSignal(t, repo.started[0], "shared metadata query")
	runtimeObserved := &runtimeObservedContext{
		Context: t.Context(),
		ready:   make(chan struct{}),
	}
	runtimeResult := startRuntimeCall(runtimeObserved, service)
	awaitRuntimeSignal(t, runtimeObserved.ready, "runtime waiter")
	attackObserved := &runtimeObservedContext{
		Context: t.Context(),
		ready:   make(chan struct{}),
	}
	attack := startAttackFlight(attackObserved, service, nil)
	awaitRuntimeSignal(t, attackObserved.ready, "attack-data waiter")
	repo.unblock(1)
	if got := awaitLifecycleResult(t, leader); got.err != nil || got.life.Config.RealRound != 1 {
		t.Fatalf("lifecycle result: %+v", got)
	}
	if got := awaitRuntimeResult(t, runtimeResult, "runtime result"); got.err != nil || got.status.Round != 1 {
		t.Fatalf("runtime result: %+v", got)
	}
	if got := awaitLifecycleAttackResult(t, attack); got.err != nil || len(got.data) != 0 {
		t.Fatalf("attack-data result: %+v", got)
	}
	if repo.calls.Load() != 1 || repo.taskCalls.Load() != 1 {
		t.Fatalf("unexpected queries: lifecycle=%d tasks=%d", repo.calls.Load(), repo.taskCalls.Load())
	}
}

func TestLifecycleDoesNotCacheCompletedReadsOrErrors(t *testing.T) {
	wantErr := errors.New("temporary lifecycle failure")
	repo := &runtimeRetryRepository{
		err: wantErr,
	}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(service.Close)
	if _, err := service.Lifecycle(t.Context()); !errors.Is(err, wantErr) {
		t.Fatalf("lifecycle error was lost: %v", err)
	}
	if status, err := service.Runtime(t.Context()); err != nil || status.Round != 7 {
		t.Fatalf("runtime reused failed read: %+v, %v", status, err)
	}
	if life, err := service.Lifecycle(t.Context()); err != nil || life.Config.RealRound != 7 {
		t.Fatalf("later lifecycle read failed: %+v, %v", life, err)
	}
	if got := repo.calls.Load(); got != 3 {
		t.Fatalf("completed read or error was cached: queries=%d, want three", got)
	}
}

func TestLifecycleChangedStartsFreshFlightAcrossMethods(t *testing.T) {
	service, repo := newLifecycleGateGame(t)
	old := startRuntimeCall(t.Context(), service)
	awaitRuntimeSignal(t, repo.started[0], "pre-change query")
	service.Changed()
	fresh := startLifecycleCall(t.Context(), service)
	awaitRuntimeSignal(t, repo.started[1], "post-change query")
	attackObserved := &runtimeObservedContext{
		Context: t.Context(),
		ready:   make(chan struct{}),
	}
	attack := startAttackFlight(attackObserved, service, nil)
	awaitRuntimeSignal(t, attackObserved.ready, "post-change attack-data waiter")
	repo.unblock(2)
	if got := awaitLifecycleResult(t, fresh); got.err != nil || got.life.Config.RealRound != 2 {
		t.Fatalf("fresh lifecycle result: %+v", got)
	}
	if got := awaitLifecycleAttackResult(t, attack); got.err != nil {
		t.Fatalf("fresh attack-data result: %+v", got)
	}
	repo.unblock(1)
	if got := awaitRuntimeResult(t, old, "pre-change runtime result"); got.err != nil || got.status.Round != 1 {
		t.Fatalf("pre-change runtime result: %+v", got)
	}
	if got := repo.calls.Load(); got != 2 {
		t.Fatalf("post-change caller missed fresh flight: queries=%d", got)
	}
}

func TestLifecycleCloseCancelsForgottenFlightsAcrossMethods(t *testing.T) {
	service, repo := newLifecycleGateGame(t)
	old := startLifecycleCall(t.Context(), service)
	awaitRuntimeSignal(t, repo.started[0], "pre-change lifecycle query")
	service.Changed()
	fresh := startAttackFlight(t.Context(), service, nil)
	awaitRuntimeSignal(t, repo.started[1], "post-change attack-data query")
	service.Close()
	if got := awaitLifecycleResult(t, old); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("forgotten lifecycle read survived Close: %v", got.err)
	}
	if got := awaitLifecycleAttackResult(t, fresh); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("current attack-data read survived Close: %v", got.err)
	}
	if _, err := service.Lifecycle(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("new lifecycle read after Close: %v", err)
	}
	if _, err := service.Runtime(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("new runtime read after Close: %v", err)
	}
	if _, err := service.AttackData(t.Context(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("new attack-data read after Close: %v", err)
	}
	if repo.calls.Load() != 2 || repo.taskCalls.Load() != 0 {
		t.Fatalf("closed callers reached storage: lifecycle=%d tasks=%d", repo.calls.Load(), repo.taskCalls.Load())
	}
}

type lifecycleCloneRepository struct {
	outbound.GameRepository

	life    domain.Lifecycle
	started chan struct{}
	release chan struct{}
}

func (r *lifecycleCloneRepository) Lifecycle(ctx context.Context) (domain.Lifecycle, error) {
	close(r.started)
	select {
	case <-r.release:
		return r.life, nil
	case <-ctx.Done():
		return domain.Lifecycle{}, ctx.Err()
	}
}

func mutableLifecycleFixture() domain.Lifecycle {
	stamp := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	return domain.Lifecycle{
		Config: domain.GameConfig{
			Rounds: new(int32(30)),
		},
		Session: domain.Session{
			PracticeStart:  new(stamp),
			FinalStart:     new(stamp.Add(time.Hour)),
			PausedAt:       new(stamp.Add(2 * time.Hour)),
			RoundStartedAt: new(stamp.Add(3 * time.Hour)),
			SnapshotRound:  new(int32(7)),
			Snapshot:       []byte(`{"round":7}`),
		},
	}
}

func TestLifecycleSharedResultHasIndependentMutableFields(t *testing.T) {
	repo := &lifecycleCloneRepository{
		life:    mutableLifecycleFixture(),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(service.Close)
	leader := startLifecycleCall(t.Context(), service)
	awaitRuntimeSignal(t, repo.started, "shared mutable lifecycle read")
	waiterObserved := &runtimeObservedContext{
		Context: t.Context(),
		ready:   make(chan struct{}),
	}
	waiter := startLifecycleCall(waiterObserved, service)
	awaitRuntimeSignal(t, waiterObserved.ready, "mutable lifecycle waiter")
	close(repo.release)
	first, second := awaitLifecycleResult(t, leader), awaitLifecycleResult(t, waiter)
	if first.err != nil || second.err != nil {
		t.Fatalf("shared lifecycle failed: %v, %v", first.err, second.err)
	}
	*first.life.Config.Rounds = 99
	*first.life.Session.PracticeStart = time.Time{}
	*first.life.Session.FinalStart = time.Time{}
	*first.life.Session.PausedAt = time.Time{}
	*first.life.Session.RoundStartedAt = time.Time{}
	*first.life.Session.SnapshotRound = 99
	first.life.Session.Snapshot[0] = 'x'
	if !reflect.DeepEqual(second.life, mutableLifecycleFixture()) {
		t.Fatal("one caller mutated another caller's lifecycle")
	}
	if !reflect.DeepEqual(repo.life, mutableLifecycleFixture()) {
		t.Fatal("one caller mutated the repository's lifecycle")
	}
}

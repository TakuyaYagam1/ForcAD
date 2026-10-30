package checker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type processObserver struct{ actions atomic.Int32 }

func (*processObserver) WorkflowStarted()                                            {}
func (*processObserver) WorkflowEnded()                                              {}
func (o *processObserver) ActionStarted()                                            { o.actions.Add(1) }
func (o *processObserver) ActionEnded()                                              { o.actions.Add(-1) }
func (*processObserver) ObserveCompleted(string, string, domain.CheckerResult, bool) {}

func TestUnreapedCheckerRetainsCapacityAndRequiresReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reaped := make(chan struct{})
		observer := &processObserver{}
		var drained atomic.Int32
		repo := newFakeRepository(payloadJSON(t, "", defaultTask()), domain.JobActionCheckOnly)
		runner := &fakeRunner{fn: func(context.Context, processchecker.Request) (processchecker.Result, error) {
			// Cleanup can fail after the workflow deadline. It still requires
			// replacement, never an ordinary completed timeout verdict.
			time.Sleep(2 * time.Second)
			return processchecker.Result{Status: processchecker.StatusCheckFailed, PendingReap: reaped}, errors.New("process cleanup incomplete")
		}}
		worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{
			WorkConcurrency: 1, MaxWorkflowDuration: time.Second, Observer: observer, OnDegraded: func() { drained.Add(1) },
		})
		if err := worker.Handle(t.Context(), testJobID); !errors.Is(err, ErrWorkerUnreaped) {
			t.Fatalf("Handle = %v, want restart failure", err)
		}
		if !worker.Degraded() || drained.Load() != 1 || len(worker.actions) != 1 || observer.actions.Load() != 1 {
			t.Fatal("unreaped checker did not close readiness/admission and retain its occupied slot")
		}
		if err := <-worker.Failures(); !errors.Is(err, ErrWorkerUnreaped) {
			t.Fatalf("worker failure = %v", err)
		}
		if err := worker.Handle(t.Context(), testJobID); !errors.Is(err, ErrWorkerUnreaped) {
			t.Fatalf("degraded worker accepted another delivery: %v", err)
		}
		_, accounted, _, _, _ := repo.snapshot()
		_, calls := runner.snapshot()
		if accounted != 0 || calls != 1 {
			t.Fatalf("unreaped workflow was acknowledged or replayed: writes=%d calls=%d", accounted, calls)
		}
		synctest.Sleep(time.Second)
		count, age, total := worker.Unreaped()
		if count != 1 || age != time.Second || total != 1 {
			t.Fatalf("unreaped metrics = %d/%s/%d", count, age, total)
		}
		close(reaped)
		synctest.Wait()
		count, age, total = worker.Unreaped()
		if count != 0 || age != 0 || total != 1 || len(worker.actions) != 0 || observer.actions.Load() != 0 {
			t.Fatal("actual late reap did not release occupancy while preserving lifetime count")
		}
		if !worker.Degraded() || !errors.Is(worker.beginAction(t.Context()), ErrWorkerUnreaped) {
			t.Fatal("late reap reopened a worker already scheduled for replacement")
		}
	})
}

func TestDegradationWakesWaitersAndTracksAllUnreapedProcesses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var drained atomic.Int32
		worker := New(nil, nil, nil, nil, Options{WorkConcurrency: 2, OnDegraded: func() { drained.Add(1) }}, nil)
		for range 2 {
			if err := worker.beginAction(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		waiting := make(chan error, 1)
		go func() { waiting <- worker.beginAction(t.Context()) }()
		synctest.Wait()
		reaped := []chan struct{}{make(chan struct{}), make(chan struct{})}
		worker.endAction(reaped[0])
		if err := <-waiting; !errors.Is(err, ErrWorkerUnreaped) {
			t.Fatalf("waiting action = %v", err)
		}
		worker.endAction(reaped[1])
		count, _, total := worker.Unreaped()
		if count != 2 || total != 2 || drained.Load() != 1 || len(worker.failures) != 1 || len(worker.actions) != 2 {
			t.Fatal("concurrent cleanup failures were lost or replacement was requested more than once")
		}
		for _, done := range reaped {
			close(done)
		}
		synctest.Wait()
		if count, _, _ := worker.Unreaped(); count != 0 || len(worker.actions) != 0 {
			t.Fatal("reaped process occupancy leaked")
		}
	})
}

func TestDegradationDuringLeaseWriteDoesNotLaunchAnotherChecker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release, reaped := make(chan struct{}), make(chan struct{}), make(chan struct{})
		repo := &recoveredRepository{fakeRepository: newFakeRepository(payloadJSON(t, "", defaultTask()), domain.JobActionCheckOnly)}
		repo.deadline = func(context.Context) (bool, error) { close(entered); <-release; return true, nil }
		runner := &fakeRunner{}
		worker := New(repo, &fakeFlagStore{}, fakeLifecycle{lifetime: 3}, runner, Options{WorkConcurrency: 2}, nil)
		if err := worker.beginAction(t.Context()); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- worker.Handle(t.Context(), testJobID) }()
		<-entered
		worker.endAction(reaped)
		close(release)
		if err := <-done; !errors.Is(err, ErrWorkerUnreaped) {
			t.Fatalf("degradation during lease I/O: %v", err)
		}
		if _, calls := runner.snapshot(); calls != 0 {
			t.Fatal("new checker launched after another action caused degradation")
		}
		close(reaped)
		synctest.Wait()
	})
}

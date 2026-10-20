package checker

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestExtraCheckDoesNotReadOrWriteFlags(t *testing.T) {
	task := defaultTask()
	task.Puts, task.Gets = 2, 2
	repository := newFakeRepository(payloadJSON(t, "check_only", task), domain.JobActionCheckOnly)
	flags := &fakeFlagStore{}
	runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		return success(request.Action), nil
	}}
	worker := makeWorker(t, repository, flags, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatal(err)
	}
	requests, _ := runner.snapshot()
	created, lookedUp := flags.snapshot()
	if len(requests) != 1 || requests[0].Action != processchecker.ActionCheck || len(created) != 0 || len(lookedUp) != 0 {
		t.Fatalf("extra CHECK touched flag workflows: requests=%v created=%d lookups=%d", requests, len(created), len(lookedUp))
	}
}

func TestExtraCheckRoundDeadlineIsInfrastructureFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repository := newFakeRepository(payloadJSON(t, "check_only", defaultTask()), domain.JobActionCheckOnly)
		job := repository.jobs[testJobID]
		expires := time.Now().Add(3 * time.Second)
		job.ExpiresAt = &expires
		repository.jobs[testJobID] = job
		runner := &fakeRunner{fn: func(ctx context.Context, _ processchecker.Request) (processchecker.Result, error) {
			<-ctx.Done()
			return processchecker.Result{}, context.Cause(ctx)
		}}
		worker := makeWorker(t, repository, &fakeFlagStore{}, runner, Options{})
		if err := worker.Handle(context.Background(), testJobID); err != nil {
			t.Fatal(err)
		}
		_, _, _, _, results := repository.snapshot()
		if len(results) != 1 || results[0].Status != domain.TaskStatusCheckFailed {
			t.Fatalf("round deadline charged as service failure: %+v", results)
		}
		_, deadlines := repository.deadlineSnapshot()
		for _, deadline := range deadlines {
			if deadline.After(expires) {
				t.Fatal("worker extended past round expiry")
			}
		}
	})
}

package checker

import (
	"context"
	"errors"
	"testing"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type recordingObserver struct {
	active    int
	completed []domain.CheckerResult
}

func (o *recordingObserver) WorkflowStarted() { o.active++ }
func (o *recordingObserver) WorkflowEnded()   { o.active-- }
func (o *recordingObserver) ActionStarted()   {}
func (o *recordingObserver) ActionEnded()     {}
func (o *recordingObserver) ObserveCompleted(_, _ string, result domain.CheckerResult, _ bool) {
	o.completed = append(o.completed, result)
}

func TestCheckerObserverCountsOnlyConfirmedCompletion(t *testing.T) {
	runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		return processchecker.Result{Action: request.Action, Status: processchecker.StatusUp}, nil
	}}
	for _, test := range []struct {
		name            string
		completeErr     error
		completeUnknown bool
		wantCount       int
		wantErr         bool
	}{
		{name: "completed", wantCount: 1},
		{name: "commit failed", completeErr: errors.New("commit failed"), wantErr: true},
		{name: "commit result lost", completeUnknown: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepository(payloadJSON(t, "", defaultTask()), domain.JobActionCheckOnly)
			repo.completeErr = test.completeErr
			repo.completeUnknown = test.completeUnknown
			repo.states[testJobID] = domain.CheckerJobState{}
			observer := &recordingObserver{}
			worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{Observer: observer})
			err := worker.Handle(t.Context(), testJobID)
			if (err != nil) != test.wantErr {
				t.Fatalf("Handle() error = %v", err)
			}
			if test.wantCount == 1 {
				if err := worker.Handle(t.Context(), testJobID); err != nil {
					t.Fatalf("redelivery Handle() error = %v", err)
				}
			}
			if observer.active != 0 || len(observer.completed) != test.wantCount {
				t.Fatalf("observer active = %d, completed = %d", observer.active, len(observer.completed))
			}
			if test.wantCount == 1 && len(observer.completed[0].Logs) != 1 {
				t.Fatalf("observed logs = %d, want 1", len(observer.completed[0].Logs))
			}
		})
	}
}

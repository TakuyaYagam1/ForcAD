package checker

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestCheckerLogsRecordActionsWithoutSecrets(t *testing.T) {
	task := defaultTask()
	task.Puts, task.Gets = 1, 1
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	getFlag := domain.Flag{
		ID: testFlagIDs[0], TeamID: testTeamID, TaskID: testTaskID, Round: 7,
		Value: "FLAG{get-secret}", PrivateData: "get-private-seed",
	}
	flags := &fakeFlagStore{byRound: map[int32]domain.Flag{5: getFlag, 6: getFlag, 7: getFlag}}
	var mu sync.Mutex
	var secrets []string
	runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		mu.Lock()
		if request.Flag != "" {
			secrets = append(secrets, request.Flag)
		}
		if request.PrivateFlagData != "" {
			secrets = append(secrets, request.PrivateFlagData)
		}
		mu.Unlock()
		return processchecker.Result{
			Action: request.Action, Status: processchecker.StatusUp,
			PublicMessage:  "public " + request.Flag + " " + request.PrivateFlagData,
			PrivateMessage: "private checker output", Command: "checker " + request.Flag,
		}, nil
	}}
	worker := makeWorker(t, repo, flags, runner, Options{})
	if err := worker.Handle(t.Context(), testJobID); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	_, _, _, _, results := repo.snapshot()
	if len(results) != 1 {
		t.Fatalf("completed results = %d, want 1", len(results))
	}
	logs := results[0].Logs
	if len(logs) != 3 {
		t.Fatalf("checker logs = %d, want CHECK, PUT and GET", len(logs))
	}
	byAction := make(map[string]domain.CheckerLog, len(logs))
	for _, log := range logs {
		byAction[log.Action] = log
		if log.Round != 7 || log.Stage != string(domain.JobActionClassic) {
			t.Errorf("log identity = round %d stage %q", log.Round, log.Stage)
		}
		if log.StartedAt.IsZero() || log.FinishedAt.Before(log.StartedAt) || log.DurationMS < 0 {
			t.Errorf("invalid checker timing: %+v", log)
		}
	}
	for _, action := range []string{"check", "put", "get"} {
		if _, ok := byAction[action]; !ok {
			t.Errorf("missing %s log", action)
		}
	}
	encoded, err := json.Marshal(logs)
	if err != nil {
		t.Fatal(err)
	}
	publicJSON := string(encoded)
	if strings.Contains(publicJSON, "private checker output") || strings.Contains(publicJSON, "checker FLAG{") {
		t.Fatalf("private checker data leaked into logs: %s", publicJSON)
	}
	mu.Lock()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(publicJSON, secret) {
			t.Errorf("checker secret %q leaked into logs: %s", secret, publicJSON)
		}
	}
	mu.Unlock()
}

func TestSuccessfulPutLogDoesNotPublishReturnedState(t *testing.T) {
	const returnedState = "synthetic-returned-state-not-the-input-seed"
	const returnedPrivate = "synthetic-pfr-private-state"
	for _, checkerType := range []string{"hackerdom", "pfr", "nfr"} {
		t.Run(checkerType, func(t *testing.T) {
			task := defaultTask()
			task.CheckerType, task.Puts, task.Gets = checkerType, 1, 0
			repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionPutsOnly)
			flags := &fakeFlagStore{}
			var inputState string
			runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
				inputState = request.PrivateFlagData
				return processchecker.Result{Action: request.Action, Status: processchecker.StatusUp,
					PublicMessage: returnedState, PrivateMessage: returnedPrivate}, nil
			}}
			worker := makeWorker(t, repo, flags, runner, Options{})
			if err := worker.Handle(t.Context(), testJobID); err != nil {
				t.Fatal(err)
			}
			_, _, _, _, results := repo.snapshot()
			if len(results) != 1 || len(results[0].Logs) != 1 {
				t.Fatalf("completed PUT logs = %+v", results)
			}
			log := results[0].Logs[0]
			if log.Status != 101 || log.PublicMessage != "PUT completed" {
				t.Fatalf("public PUT result = %+v", log)
			}
			stored, _ := flags.snapshot()
			if len(stored) != 1 {
				t.Fatalf("stored flags = %d, want 1", len(stored))
			}
			wantPublic, wantPrivate := "", returnedState
			switch checkerType {
			case "pfr":
				wantPublic, wantPrivate = returnedState, returnedPrivate
			case "nfr":
				wantPrivate = inputState
			}
			if stored[0].PublicData != wantPublic || stored[0].PrivateData != wantPrivate {
				t.Fatal("public hints or private retrieval state changed while sanitizing logs")
			}
		})
	}
}

func TestCheckerLogRecordsTimeout(t *testing.T) {
	task := defaultTask()
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionCheckOnly)
	runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		return processchecker.Result{
			Action: request.Action, Status: processchecker.StatusDown,
			PublicMessage: "checker timed out", TimedOut: true,
		}, nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	if err := worker.Handle(t.Context(), testJobID); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	_, _, _, _, results := repo.snapshot()
	if len(results) != 1 || len(results[0].Logs) != 1 {
		t.Fatalf("completed checker logs = %+v", results)
	}
	log := results[0].Logs[0]
	if !log.TimedOut || log.Status != int32(processchecker.StatusDown) || log.Action != string(processchecker.ActionCheck) {
		t.Fatalf("timeout log = %+v", log)
	}
	if log.FinishedAt.IsZero() || log.StartedAt.IsZero() || time.Since(log.FinishedAt) < 0 {
		t.Fatalf("timeout timing = %+v", log)
	}
}

func TestCheckerLogPreservesInitialStopCause(t *testing.T) {
	for _, test := range []struct {
		name        string
		result      processchecker.Result
		wantTimeout bool
	}{
		{"output limit before deadline", processchecker.Result{Status: processchecker.StatusCheckFailed, OutputLimited: true}, false},
		{"cancel before deadline", processchecker.Result{Status: processchecker.StatusDown, Canceled: true}, false},
		{"timeout before output limit", processchecker.Result{Status: processchecker.StatusDown, TimedOut: true, OutputLimited: true}, true},
		{"runner only returned a deadline error", processchecker.Result{}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			log := checkerActionLog(domain.CheckerJob{Round: 1}, processchecker.ActionPut, nil,
				now, now, test.result, context.DeadlineExceeded)
			if log.TimedOut != test.wantTimeout {
				t.Fatalf("log timeout = %v, want %v: %+v", log.TimedOut, test.wantTimeout, log)
			}
			if test.result.OutputLimited && !test.wantTimeout && log.Status != 110 {
				t.Fatalf("output limit became a service timeout: %+v", log)
			}
		})
	}
}

package checker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

var (
	testJobID   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	secondJobID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	testTeamID  = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	testTaskID  = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	testFlagIDs = [3]uuid.UUID{
		uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		uuid.MustParse("77777777-7777-4777-8777-777777777777"),
	}
)

type fakeRepository struct {
	mu sync.Mutex

	jobs             map[uuid.UUID]domain.CheckerJob
	states           map[uuid.UUID]domain.CheckerJobState
	activeTeams      map[uuid.UUID]bool
	claimErr         error
	completeErr      error
	completeUnknown  bool
	completeOK       bool
	completeCalls    int
	renewCalls       int
	loseAfterRenew   int
	deadlineCalls    int
	deadlines        []time.Time
	deadlineOK       bool
	accountingWrites int
	results          []domain.CheckerResult
	claimAt          time.Time
}

func newFakeRepository(payload []byte, workflow domain.JobAction) *fakeRepository {
	return &fakeRepository{
		jobs: map[uuid.UUID]domain.CheckerJob{
			testJobID: {ID: testJobID, Round: 7, Action: workflow, TeamID: testTeamID, TaskID: testTaskID, Payload: payload},
		},
		states:      make(map[uuid.UUID]domain.CheckerJobState),
		activeTeams: map[uuid.UUID]bool{testTeamID: true},
		completeOK:  true,
		deadlineOK:  true,
	}
}

func (r *fakeRepository) Claim(_ context.Context, jobID uuid.UUID, owner string, at, _ time.Time) (domain.CheckerJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimAt = at
	if r.claimErr != nil {
		return domain.CheckerJob{}, r.claimErr
	}
	job, ok := r.jobs[jobID]
	if !ok {
		return domain.CheckerJob{}, domain.ErrCheckerJobClosed
	}
	if state := r.states[jobID]; state.FinishedAt != nil {
		return domain.CheckerJob{}, domain.ErrCheckerJobClosed
	}
	job.ClaimToken = &owner
	r.jobs[jobID] = job
	return job, nil
}

func (r *fakeRepository) State(_ context.Context, jobID uuid.UUID) (domain.CheckerJobState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[jobID]
	if !ok {
		return domain.CheckerJobState{}, domain.ErrCheckerJobClosed
	}
	return state, nil
}

func (r *fakeRepository) Renew(_ context.Context, _ uuid.UUID, _ string, _, _ time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renewCalls++
	return r.loseAfterRenew == 0 || r.renewCalls <= r.loseAfterRenew, nil
}

func (r *fakeRepository) SetDeadline(_ context.Context, _ uuid.UUID, _ string, _, deadline time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deadlineCalls++
	r.deadlines = append(r.deadlines, deadline)
	return r.deadlineOK && deadline.After(time.Now()), nil
}

func (r *fakeRepository) ExtendDeadline(ctx context.Context, jobID uuid.UUID, owner string, at, deadline time.Time) (bool, error) {
	return r.SetDeadline(ctx, jobID, owner, at, deadline)
}

func (r *fakeRepository) Complete(_ context.Context, jobID uuid.UUID, _ string, at time.Time, result domain.CheckerResult) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completeCalls++
	if r.completeUnknown {
		r.accountingWrites++
		r.results = append(r.results, result)
		r.states[jobID] = domain.CheckerJobState{FinishedAt: &at}
		return false, errors.New("commit result was lost")
	}
	if r.completeErr != nil {
		return false, r.completeErr
	}
	if !r.completeOK {
		return false, nil
	}
	r.accountingWrites++
	r.results = append(r.results, result)
	r.states[jobID] = domain.CheckerJobState{FinishedAt: &at}
	return true, nil
}

func (r *fakeRepository) Expire(_ context.Context, jobID uuid.UUID, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[jobID]
	if !ok || state.FinishedAt != nil || state.DeadlineAt == nil || state.DeadlineAt.After(at) {
		return false, nil
	}
	state.FinishedAt = &at
	r.states[jobID] = state
	r.accountingWrites++
	return true, nil
}
func (r *fakeRepository) IsActiveTeam(_ context.Context, teamID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeTeams[teamID], nil
}

func (r *fakeRepository) setState(jobID uuid.UUID, state domain.CheckerJobState) {
	r.mu.Lock()
	r.states[jobID] = state
	r.mu.Unlock()
}

func (r *fakeRepository) snapshot() (complete, accounted, renewals, deadlineExtensions int, results []domain.CheckerResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completeCalls, r.accountingWrites, r.renewCalls, r.deadlineCalls, append([]domain.CheckerResult(nil), r.results...)
}

func (r *fakeRepository) deadlineSnapshot() (time.Time, []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.claimAt, append([]time.Time(nil), r.deadlines...)
}

type fakeFlagStore struct {
	mu          sync.Mutex
	created     []domain.Flag
	byRound     map[int32]domain.Flag
	selected    []int32
	createError error
}

func (f *fakeFlagStore) Create(_ context.Context, flag domain.Flag, owner *domain.CheckerJobOwner) (domain.Flag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createError != nil {
		return domain.Flag{}, f.createError
	}
	if owner == nil || owner.JobID == uuid.Nil() || owner.ClaimToken == "" {
		return domain.Flag{}, errors.New("missing ownership fence")
	}
	f.created = append(f.created, flag)
	return flag, nil
}

func (f *fakeFlagStore) FindRandomRound(_ context.Context, _, _ uuid.UUID, round int32) (domain.Flag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.selected = append(f.selected, round)
	flag, ok := f.byRound[round]
	if !ok {
		return domain.Flag{}, domain.ErrFlagNotFound
	}
	return flag, nil
}

func (f *fakeFlagStore) snapshot() ([]domain.Flag, []int32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Flag(nil), f.created...), append([]int32(nil), f.selected...)
}

type fakeLifecycle struct{ lifetime int32 }

func (l fakeLifecycle) Lifecycle(context.Context) (domain.Lifecycle, error) {
	return domain.Lifecycle{Config: domain.GameConfig{FlagLifetime: l.lifetime}}, nil
}

type fakeRunner struct {
	mu       sync.Mutex
	fn       func(context.Context, processchecker.Request) (processchecker.Result, error)
	requests []processchecker.Request
	current  int
	maximum  int
}

func (r *fakeRunner) Run(ctx context.Context, request processchecker.Request) (processchecker.Result, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	r.current++
	if r.current > r.maximum {
		r.maximum = r.current
	}
	fn := r.fn
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.current--
		r.mu.Unlock()
	}()
	return fn(ctx, request)
}

func (r *fakeRunner) snapshot() ([]processchecker.Request, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]processchecker.Request(nil), r.requests...), r.maximum
}

func payloadJSON(t *testing.T, workflow string, task domain.Task) []byte {
	t.Helper()
	payload := domain.JobPayload{
		Team: domain.Team{ID: testTeamID, Name: "red", IP: "192.0.2.5", Token: "0123456789abcdef", Active: true},
		Task: task, Workflow: workflow,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func makeWorker(t *testing.T, repo *fakeRepository, flags *fakeFlagStore, runner *fakeRunner, options Options) *Worker {
	t.Helper()
	return New(repo, flags, fakeLifecycle{lifetime: 3}, runner, options, nil)
}

func TestNewUsesJobGraceRenewalDefaultAndConfiguredActionLimit(t *testing.T) {
	task := defaultTask()
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	worker := makeWorker(t, repo, &fakeFlagStore{}, &fakeRunner{}, Options{WorkConcurrency: 5})
	if worker.options.LeaseDuration != 30*time.Second || worker.options.RenewInterval != 10*time.Second ||
		worker.options.RecoveryGrace != 30*time.Second || worker.options.MaxWorkflowDuration != 30*time.Minute {
		t.Fatalf("worker defaults = lease %s, renew %s, recovery %s, workflow %s", worker.options.LeaseDuration,
			worker.options.RenewInterval, worker.options.RecoveryGrace, worker.options.MaxWorkflowDuration)
	}
	if cap(worker.actions) != 5 {
		t.Fatalf("action capacity = %d, want configured worker concurrency 5", cap(worker.actions))
	}
}

func TestHandleRejectsNilJobIDBeforeClaiming(t *testing.T) {
	repo := newFakeRepository(payloadJSON(t, "", defaultTask()), domain.JobActionClassic)
	worker := makeWorker(t, repo, &fakeFlagStore{}, &fakeRunner{}, Options{})
	err := worker.Handle(context.Background(), uuid.Nil())
	if err == nil || !strings.HasPrefix(err.Error(), "checker.Worker.Handle - ") {
		t.Fatalf("Handle() error = %v, want a located nil-ID error", err)
	}
	claimAt, _ := repo.deadlineSnapshot()
	if !claimAt.IsZero() {
		t.Fatalf("nil job ID reached repository claim at %s", claimAt)
	}
}

func defaultTask() domain.Task {
	return domain.Task{ID: testTaskID, Name: "service", Checker: "/tmp/checker", CheckerTimeout: 1, Places: 3, CheckerType: "hackerdom"}
}

func TestDecodePayloadAcceptsCurrentJobPayload(t *testing.T) {
	data := payloadJSON(t, "classic", defaultTask())
	payload, err := decodePayload(data)
	if err != nil {
		t.Fatalf("decode current job payload: %v", err)
	}
	if payload.Team.ID != testTeamID || payload.Team.Token != "0123456789abcdef" || payload.Task.ID != testTaskID || payload.Task.CheckerTimeout != 1 {
		t.Fatalf("decoded job payload = %+v", payload)
	}
}

func TestDecodePayloadIgnoresUnknownFields(t *testing.T) {
	data := fmt.Appendf(nil, `{"team":{"id":%q,"name":"red","ip":"192.0.2.5","token":"token","active":true,"future_team_field":true},"task":{"id":%q,"name":"service","checker":"/tmp/checker","checker_timeout":1,"places":3,"checker_type":"hackerdom","future_task_field":true},"workflow":"classic","generation":4,"future_root_field":true}`, testTeamID.String(), testTaskID.String())
	payload, err := decodePayload(data)
	if err != nil {
		t.Fatalf("decode payload with forward-compatible fields: %v", err)
	}
	if payload.Team.ID != testTeamID || payload.Team.IP != "192.0.2.5" || payload.Team.Token != "token" ||
		payload.Task.ID != testTaskID || payload.Task.CheckerTimeout != 1 || payload.Workflow != "classic" || payload.Generation != 4 {
		t.Fatalf("decoded payload = %+v", payload)
	}
}

func TestDecodePayloadDoesNotMatchLegacyCaseVariants(t *testing.T) {
	data := fmt.Appendf(nil, `{"TEAM":{"id":%q},"task":{"CHECKER_TIMEOUT":1}}`, testTeamID.String())
	payload, err := decodePayload(data)
	if err != nil {
		t.Fatalf("decode payload with unknown case variants: %v", err)
	}
	if payload.Team.ID != uuid.Nil() || payload.Task.CheckerTimeout != 0 {
		t.Fatalf("case variants populated canonical fields: %+v", payload)
	}
}

func TestDecodePayloadRejectsDuplicateMembersAndInvalidUTF8(t *testing.T) {
	invalidUTF8 := []byte(`{"team":{"ip":"`)
	invalidUTF8 = append(invalidUTF8, 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"},"task":{"checker_timeout":1}}`)...)
	tests := []struct {
		name string
		data []byte
	}{
		{name: "duplicate member", data: fmt.Appendf(nil, `{"team":{"id":%q,"id":%q},"task":{"checker_timeout":1}}`, testTeamID.String(), testTeamID.String())},
		{name: "invalid UTF-8", data: invalidUTF8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodePayload(test.data); err == nil {
				t.Fatal("decodePayload() accepted ambiguous or invalid JSON")
			}
		})
	}
}

func TestMalformedPayloadCompletesCheckFailedWithoutRunningChecker(t *testing.T) {
	valid := payloadJSON(t, "", defaultTask())
	teamIDField := fmt.Appendf(nil, `"id":%q`, testTeamID.String())
	duplicateTeamID := fmt.Appendf(nil, `"id":%q,"id":%q`, testTeamID.String(), testTeamID.String())
	duplicateID := bytes.Replace(valid, teamIDField, duplicateTeamID, 1)
	badName := append([]byte(`"name":"`), 0xff)
	badName = append(badName, '"')
	invalidUTF8 := bytes.Replace(valid, []byte(`"name":"red"`), badName, 1)
	for _, test := range []struct {
		name    string
		payload []byte
	}{
		{name: "duplicate member", payload: duplicateID},
		{name: "invalid UTF-8", payload: invalidUTF8},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepository(test.payload, domain.JobActionClassic)
			runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
				return success(request.Action), nil
			}}
			flags := &fakeFlagStore{}
			worker := makeWorker(t, repo, flags, runner, Options{})
			if err := worker.Handle(context.Background(), testJobID); err != nil {
				t.Fatalf("Handle() malformed payload: %v", err)
			}
			requests, _ := runner.snapshot()
			created, selected := flags.snapshot()
			complete, accounted, _, _, results := repo.snapshot()
			if len(requests) != 0 || len(created) != 0 || len(selected) != 0 {
				t.Fatalf("malformed payload reached checker or flag store: requests=%d created=%d selected=%d", len(requests), len(created), len(selected))
			}
			if complete != 1 || accounted != 1 || len(results) != 1 || results[0].Status != int32(processchecker.StatusCheckFailed) {
				t.Fatalf("malformed payload result = complete %d, accounted %d, %#v; want one CHECK_FAILED result", complete, accounted, results)
			}
			if results[0].Status == int32(processchecker.StatusUp) || results[0].Status == int32(processchecker.StatusDown) {
				t.Fatalf("malformed payload produced UP or DOWN: result=%#v", results[0])
			}
		})
	}
}

func TestPlanWorkflowRejectsNilOrMismatchedPayloadIDs(t *testing.T) {
	marshalPayload := func(teamID, taskID uuid.UUID) []byte {
		payload := domain.JobPayload{
			Team: domain.Team{ID: teamID, IP: "192.0.2.5", Token: "0123456789abcdef", Active: true},
			Task: defaultTask(),
		}
		payload.Task.ID = taskID
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, test := range []struct {
		name string
		job  domain.CheckerJob
	}{
		{name: "nil team ID", job: domain.CheckerJob{ID: testJobID, Action: domain.JobActionClassic, TeamID: testTeamID, TaskID: testTaskID, Payload: marshalPayload(uuid.Nil(), testTaskID)}},
		{name: "nil task ID", job: domain.CheckerJob{ID: testJobID, Action: domain.JobActionClassic, TeamID: testTeamID, TaskID: testTaskID, Payload: marshalPayload(testTeamID, uuid.Nil())}},
		{name: "mismatched team ID", job: domain.CheckerJob{ID: testJobID, Action: domain.JobActionClassic, TeamID: secondJobID, TaskID: testTaskID, Payload: payloadJSON(t, "", defaultTask())}},
		{name: "mismatched task ID", job: domain.CheckerJob{ID: testJobID, Action: domain.JobActionClassic, TeamID: testTeamID, TaskID: secondJobID, Payload: payloadJSON(t, "", defaultTask())}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if plan := planWorkflow(test.job); plan.isValid || plan.failure.Status != workflowTimeoutStatus {
				t.Fatalf("planWorkflow() = %+v, want invalid workflow verdict", plan)
			}
		})
	}
}

func TestMissingJobActionDoesNotFallBackToPayloadWorkflow(t *testing.T) {
	repo := newFakeRepository(payloadJSON(t, "classic", defaultTask()), domain.JobActionClassic)
	job := repo.jobs[testJobID]
	job.Action = ""
	repo.jobs[testJobID] = job
	runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		t.Errorf("checker ran with action %s", request.Action)
		return success(request.Action), nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatalf("Handle() missing action: %v", err)
	}
	requests, _ := runner.snapshot()
	complete, _, _, _, results := repo.snapshot()
	if len(requests) != 0 {
		t.Fatalf("missing action reached checker %d times", len(requests))
	}
	if complete != 1 || len(results) != 1 || results[0].Status != workflowTimeoutStatus {
		t.Fatalf("missing action result = complete %d, %#v; want one CHECK_FAILED completion", complete, results)
	}
}

func TestWorkflowActionCountsAreBoundedWithoutTruncation(t *testing.T) {
	cases := []struct {
		name     string
		workflow domain.JobAction
		puts     int32
		gets     int32
		want     bool
	}{
		{name: "classic maximum", workflow: domain.JobActionClassic, puts: 512, gets: 512, want: true},
		{name: "puts only maximum", workflow: domain.JobActionPutsOnly, puts: maxWorkflowActions, want: true},
		{name: "check gets maximum", workflow: domain.JobActionCheckGets, gets: maxWorkflowActions, want: true},
		{name: "classic over maximum", workflow: domain.JobActionClassic, puts: 1024, gets: 1},
		{name: "large put count", workflow: domain.JobActionPutsOnly, puts: 1<<31 - 1},
		{name: "negative get count", workflow: domain.JobActionCheckGets, gets: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validWorkflowActionCount(tc.workflow, domain.Task{Puts: tc.puts, Gets: tc.gets})
			if got != tc.want {
				t.Fatalf("validWorkflowActionCount(%q, puts=%d, gets=%d) = %t, want %t", tc.workflow, tc.puts, tc.gets, got, tc.want)
			}
		})
	}
}

func TestOversizedActionCountCompletesAsCheckFailedWithoutRunningChecker(t *testing.T) {
	task := defaultTask()
	task.Puts = 1<<31 - 1
	repo := newFakeRepository(payloadJSON(t, "puts_only", task), domain.JobActionPutsOnly)
	runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		return success(request.Action), nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{WorkConcurrency: 2})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatalf("Handle() oversized workflow: %v", err)
	}
	requests, _ := runner.snapshot()
	_, accounted, _, _, results := repo.snapshot()
	if len(requests) != 0 {
		t.Fatalf("runner received %d actions for oversized workflow", len(requests))
	}
	if accounted != 1 || len(results) != 1 || results[0].Status != workflowTimeoutStatus || results[0].PrivateMessage != "Invalid checker workflow action count" {
		t.Fatalf("invalid payload completion = accounted %d, %#v; want one status 110 result", accounted, results)
	}
}

func success(action processchecker.Action) processchecker.Result {
	return processchecker.Result{Action: action, Status: processchecker.StatusUp, PublicMessage: string(action) + " public", PrivateMessage: string(action) + " private", Command: string(action) + " 192.0.2.5"}
}

func TestClassicRunsParallelPutsAndSequentialGetsAfterCheck(t *testing.T) {
	task := defaultTask()
	task.Puts, task.Gets, task.CheckerType = 3, 2, "hackerdom_pfr"
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	flags := &fakeFlagStore{byRound: map[int32]domain.Flag{
		5: {ID: testFlagIDs[0], TeamID: testTeamID, TaskID: testTaskID, Round: 5, Value: "SFLAG5=", PrivateData: "get-seed-5", VulnerabilityID: int32ptr(1)},
		6: {ID: testFlagIDs[1], TeamID: testTeamID, TaskID: testTaskID, Round: 6, Value: "SFLAG6=", PrivateData: "get-seed-6", VulnerabilityID: int32ptr(2)},
		7: {ID: testFlagIDs[2], TeamID: testTeamID, TaskID: testTaskID, Round: 7, Value: "SFLAG7=", PrivateData: "get-seed-7", VulnerabilityID: int32ptr(3)},
	}}
	putStarts := make(chan struct{}, 3)
	getStarts := make(chan int, 2)
	putRelease := make(chan struct{})
	getRelease := make(chan struct{})
	var getNumber atomic.Int32
	var checkComplete atomic.Bool
	runner := &fakeRunner{fn: func(ctx context.Context, request processchecker.Request) (processchecker.Result, error) {
		switch request.Action {
		case processchecker.ActionCheck:
			checkComplete.Store(true)
			return processchecker.Result{Action: request.Action, Status: processchecker.StatusUp, PublicMessage: "check output"}, nil
		case processchecker.ActionPut:
			if !checkComplete.Load() {
				t.Error("PUT started before CHECK completed")
			}
			putStarts <- struct{}{}
			select {
			case <-putRelease:
				return processchecker.Result{Action: request.Action, Status: processchecker.StatusUp, PublicMessage: "put-public", PrivateMessage: "put-private"}, nil
			case <-ctx.Done():
				return processchecker.Result{}, context.Cause(ctx)
			}
		case processchecker.ActionGet:
			if !checkComplete.Load() {
				t.Error("GET started before CHECK completed")
			}
			n := int(getNumber.Add(1))
			getStarts <- n
			if n == 1 {
				select {
				case <-getRelease:
				case <-ctx.Done():
					return processchecker.Result{}, context.Cause(ctx)
				}
			}
			return success(request.Action), nil
		default:
			return processchecker.Result{}, fmt.Errorf("unexpected action %q", request.Action)
		}
	}}
	worker := makeWorker(t, repo, flags, runner, Options{LeaseDuration: time.Second, RenewInterval: 100 * time.Millisecond, WorkConcurrency: 4})
	done := make(chan error, 1)
	go func() { done <- worker.Handle(context.Background(), testJobID) }()

	for range task.Puts {
		waitSignal(t, putStarts, "parallel PUT start")
	}
	if got := waitValue(t, getStarts, "first GET start"); got != 1 {
		t.Fatalf("first GET sequence = %d, want 1", got)
	}
	select {
	case n := <-getStarts:
		t.Fatalf("GET %d started before prior GET completed", n)
	case <-time.After(25 * time.Millisecond):
	}
	close(getRelease)
	if got := waitValue(t, getStarts, "second GET start"); got != 2 {
		t.Fatalf("second GET sequence = %d, want 2", got)
	}
	close(putRelease)
	if err := waitError(t, done); err != nil {
		t.Fatal(err)
	}

	complete, accounted, _, deadlineCalls, results := repo.snapshot()
	if complete != 1 || accounted != 1 || len(results) != 1 || results[0].Status != int32(processchecker.StatusUp) {
		t.Fatalf("durable completion = calls %d accounting %d results %#v", complete, accounted, results)
	}
	if results[0].PublicMessage != "check output" {
		t.Fatalf("successful classic workflow result = %#v, want CHECK verdict", results[0])
	}
	requests, maximum := runner.snapshot()
	if len(requests) != 6 {
		t.Fatalf("runner received %d actions, want CHECK + 3 PUT + 2 GET", len(requests))
	}
	if maximum < 3 {
		t.Fatalf("max concurrent checker actions = %d, want parallel PUTs", maximum)
	}
	if deadlineCalls < len(requests) {
		t.Fatalf("deadline extensions = %d, want at least one per action (%d)", deadlineCalls, len(requests))
	}
	created, selectedRounds := flags.snapshot()
	if len(created) != 3 || len(selectedRounds) != 2 {
		t.Fatalf("stored PUT flags %d, selected GET rounds %v", len(created), selectedRounds)
	}
	for _, round := range selectedRounds {
		if round < 5 || round > 7 {
			t.Errorf("GET selected round %d outside live flag rounds [5,7]", round)
		}
	}
	for _, flag := range created {
		if !regexp.MustCompile(`^S[A-Z0-9]{30}=$`).MatchString(flag.Value) {
			t.Errorf("generated flag has invalid protocol shape: %q", flag.Value)
		}
		if flag.PrivateData != "put-private" {
			t.Errorf("pfr private flag data = %q, want checker stderr", flag.PrivateData)
		}
		if flag.PublicData != "put-public" {
			t.Errorf("pfr public flag data = %q, want checker stdout", flag.PublicData)
		}
		if flag.VulnerabilityID == nil || *flag.VulnerabilityID < 1 || *flag.VulnerabilityID > task.Places {
			t.Errorf("vulnerability ID = %v, want 1..%d", flag.VulnerabilityID, task.Places)
		}
	}
}

func TestPutsCoverEachPlaceBeforeRepeating(t *testing.T) {
	for _, tc := range []struct {
		name   string
		puts   int32
		places int32
		want   int
	}{
		{name: "ten places", puts: 10, places: 10, want: 1},
		{name: "twenty puts", puts: 20, places: 10, want: 2},
		{name: "one put", puts: 1, places: 10, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := defaultTask()
			task.Puts, task.Places = tc.puts, tc.places
			repo := newFakeRepository(payloadJSON(t, "puts_only", task), domain.JobActionPutsOnly)
			flags := &fakeFlagStore{}
			runner := &fakeRunner{fn: func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
				return success(request.Action), nil
			}}
			worker := makeWorker(t, repo, flags, runner, Options{WorkConcurrency: 4})
			if err := worker.Handle(t.Context(), testJobID); err != nil {
				t.Fatal(err)
			}
			created, _ := flags.snapshot()
			if len(created) != int(tc.puts) {
				t.Fatalf("stored %d flags, want %d", len(created), tc.puts)
			}
			requests, _ := runner.snapshot()
			if len(requests) != int(tc.puts) {
				t.Fatalf("checker received %d PUT requests, want %d", len(requests), tc.puts)
			}
			requestPlaces := make(map[string]int)
			for _, request := range requests {
				if request.Action != processchecker.ActionPut {
					t.Fatalf("unexpected checker action %q", request.Action)
				}
				requestPlaces[request.Flag] = request.VulnNumber
			}
			counts := make(map[int32]int)
			for _, flag := range created {
				if flag.VulnerabilityID == nil || *flag.VulnerabilityID < 1 || *flag.VulnerabilityID > tc.places {
					t.Fatalf("invalid vulnerability ID: %v", flag.VulnerabilityID)
				}
				if requestPlaces[flag.Value] != int(*flag.VulnerabilityID) {
					t.Errorf("checker received place %d for stored place %d", requestPlaces[flag.Value], *flag.VulnerabilityID)
				}
				counts[*flag.VulnerabilityID]++
			}
			if tc.want == 0 {
				return
			}
			for place := int32(1); place <= tc.places; place++ {
				if counts[place] != tc.want {
					t.Errorf("place %d received %d flags, want %d", place, counts[place], tc.want)
				}
			}
		})
	}
}

func TestCheckFailureSkipsAllFollowupActions(t *testing.T) {
	task := defaultTask()
	task.Puts, task.Gets = 2, 2
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	flags := &fakeFlagStore{}
	var calls atomic.Int32
	runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
		calls.Add(1)
		return processchecker.Result{Action: req.Action, Status: processchecker.StatusDown, PublicMessage: "checker down", PrivateMessage: "detail", Command: "check"}, nil
	}}
	worker := makeWorker(t, repo, flags, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("checker actions = %d, want only CHECK", got)
	}
	_, _, _, _, results := repo.snapshot()
	if len(results) != 1 || results[0].Status != int32(processchecker.StatusDown) || results[0].PublicMessage != "checker down" {
		t.Fatalf("stored failed CHECK = %#v", results)
	}
	created, rounds := flags.snapshot()
	if len(created) != 0 || len(rounds) != 0 {
		t.Fatalf("failed CHECK produced flags %d or GET lookups %v", len(created), rounds)
	}
}

func TestCheckGetsStopsAtFirstFailedGet(t *testing.T) {
	task := defaultTask()
	task.Puts, task.Gets = 4, 3
	repo := newFakeRepository(payloadJSON(t, "check_gets", task), domain.JobActionCheckGets)
	flags := &fakeFlagStore{byRound: map[int32]domain.Flag{
		5: {ID: testFlagIDs[0], TeamID: testTeamID, TaskID: testTaskID, Round: 5, Value: "SFLAG5=", PrivateData: "seed", VulnerabilityID: int32ptr(1)},
		6: {ID: testFlagIDs[1], TeamID: testTeamID, TaskID: testTaskID, Round: 6, Value: "SFLAG6=", PrivateData: "seed", VulnerabilityID: int32ptr(1)},
		7: {ID: testFlagIDs[2], TeamID: testTeamID, TaskID: testTaskID, Round: 7, Value: "SFLAG7=", PrivateData: "seed", VulnerabilityID: int32ptr(1)},
	}}
	var calls []processchecker.Action
	var mu sync.Mutex
	runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
		mu.Lock()
		calls = append(calls, req.Action)
		mu.Unlock()
		if req.Action == processchecker.ActionGet {
			return processchecker.Result{Action: req.Action, Status: processchecker.StatusCorrupt, PublicMessage: "GET failed"}, nil
		}
		return success(req.Action), nil
	}}
	worker := makeWorker(t, repo, flags, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != processchecker.ActionCheck || calls[1] != processchecker.ActionGet {
		t.Fatalf("check_gets actions = %v, want CHECK then one GET", calls)
	}
	_, rounds := flags.snapshot()
	if len(rounds) != 1 {
		t.Fatalf("GET lookups = %v, want chain to stop after failure", rounds)
	}
	requests, _ := runner.snapshot()
	if len(requests) != 2 || requests[1].Action != processchecker.ActionGet || requests[1].TeamIP != "192.0.2.5" {
		t.Fatalf("GET request = %#v, want checker request for the team", requests)
	}
	var foundRequestFlag bool
	for round, flag := range flags.byRound {
		if rounds[0] == round && requests[1].Flag == flag.Value && requests[1].PrivateFlagData == flag.PrivateData &&
			requests[1].VulnNumber == int(*flag.VulnerabilityID) {
			foundRequestFlag = true
		}
	}
	if !foundRequestFlag {
		t.Fatalf("GET checker args do not match randomly selected round %d", rounds[0])
	}
	_, _, _, _, results := repo.snapshot()
	if len(results) != 1 || results[0].Status != int32(processchecker.StatusCorrupt) || results[0].PublicMessage != "GET failed" {
		t.Fatalf("stored check_gets result = %#v", results)
	}
}

func TestClassicPutFailureTakesPrecedenceOverEarlierGetFailure(t *testing.T) {
	task := defaultTask()
	task.Puts, task.Gets = 1, 1
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	flags := &fakeFlagStore{byRound: map[int32]domain.Flag{
		5: {ID: testFlagIDs[0], TeamID: testTeamID, TaskID: testTaskID, Round: 5, Value: "SFLAG5=", PrivateData: "seed"},
		6: {ID: testFlagIDs[1], TeamID: testTeamID, TaskID: testTaskID, Round: 6, Value: "SFLAG6=", PrivateData: "seed"},
		7: {ID: testFlagIDs[2], TeamID: testTeamID, TaskID: testTaskID, Round: 7, Value: "SFLAG7=", PrivateData: "seed"},
	}}
	getFinished := make(chan struct{})
	runner := &fakeRunner{fn: func(ctx context.Context, req processchecker.Request) (processchecker.Result, error) {
		switch req.Action {
		case processchecker.ActionCheck:
			return success(req.Action), nil
		case processchecker.ActionGet:
			close(getFinished)
			return processchecker.Result{Action: req.Action, Status: processchecker.StatusCorrupt, PublicMessage: "GET failed"}, nil
		case processchecker.ActionPut:
			select {
			case <-getFinished:
				return processchecker.Result{Action: req.Action, Status: processchecker.StatusDown, PublicMessage: "PUT failed"}, nil
			case <-ctx.Done():
				return processchecker.Result{}, context.Cause(ctx)
			}
		default:
			return processchecker.Result{}, fmt.Errorf("unexpected action %q", req.Action)
		}
	}}
	worker := makeWorker(t, repo, flags, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, results := repo.snapshot()
	if len(results) != 1 || results[0].Status != int32(processchecker.StatusDown) || results[0].PublicMessage != "PUT failed" {
		t.Fatalf("classic verdict = %#v, want failed PUT before failed GET", results)
	}
}

func TestPutsOnlyTransformsSuccessfulPublicOutputAndRespectsFlagTags(t *testing.T) {
	tests := []struct {
		name        string
		tags        string
		wantPublic  string
		wantPrivate string
	}{
		{name: "pfr", tags: "hackerdom_pfr", wantPublic: "PUT output", wantPrivate: "PUT stderr"},
		{name: "default", tags: "hackerdom", wantPrivate: "PUT output"},
		{name: "nfr", tags: "hackerdom_nfr", wantPrivate: "PUT seed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := defaultTask()
			task.Puts, task.CheckerType = 1, test.tags
			repo := newFakeRepository(payloadJSON(t, "puts_only", task), domain.JobActionPutsOnly)
			flags := &fakeFlagStore{}
			var requestSeed string
			runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
				if req.Action != processchecker.ActionPut {
					t.Errorf("puts_only ran %s", req.Action)
				}
				if len(req.PrivateFlagData) != 40 {
					t.Errorf("PUT private seed length = %d, want 40 hex chars", len(req.PrivateFlagData))
				}
				requestSeed = req.PrivateFlagData
				return processchecker.Result{Action: req.Action, Status: processchecker.StatusUp, PublicMessage: "PUT output", PrivateMessage: "PUT stderr"}, nil
			}}
			worker := makeWorker(t, repo, flags, runner, Options{})
			if err := worker.Handle(context.Background(), testJobID); err != nil {
				t.Fatal(err)
			}
			created, _ := flags.snapshot()
			wantPrivate := test.wantPrivate
			if test.name == "nfr" {
				wantPrivate = requestSeed
			}
			if len(created) != 1 || created[0].PublicData != test.wantPublic || created[0].PrivateData != wantPrivate {
				t.Fatalf("persisted flags = %#v", created)
			}
			_, _, _, _, results := repo.snapshot()
			if len(results) != 1 || results[0].PublicMessage != "OK" || results[0].Status != int32(processchecker.StatusUp) {
				t.Fatalf("puts_only completion = %#v, want successful OK", results)
			}
		})
	}
}

func TestCheckerTimeoutVerdictIsDurablyCompleted(t *testing.T) {
	task := defaultTask()
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
		return processchecker.Result{Action: req.Action, Status: processchecker.StatusDown, PublicMessage: "Checker timed out", PrivateMessage: "CHECK timeout (killed by ForcAD)", TimedOut: true}, nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatal(err)
	}
	_, accounted, _, _, results := repo.snapshot()
	if accounted != 1 || len(results) != 1 || results[0].Status != int32(processchecker.StatusDown) {
		t.Fatalf("timeout accounting = %d, results %#v", accounted, results)
	}
}

func TestLeaseLossCancelsCheckerAndDoesNotComplete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		task := defaultTask()
		repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
		repo.loseAfterRenew = 1
		started := make(chan struct{})
		canceled := make(chan struct{})
		runner := &fakeRunner{fn: func(ctx context.Context, _ processchecker.Request) (processchecker.Result, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return processchecker.Result{}, context.Cause(ctx)
		}}
		worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{LeaseDuration: 40 * time.Millisecond, RenewInterval: 5 * time.Millisecond, WorkConcurrency: 1})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- worker.Handle(ctx, testJobID) }()
		waitSignal(t, started, "checker start")
		synctest.Sleep(4 * time.Millisecond)
		if _, _, renewals, _, _ := repo.snapshot(); renewals != 1 {
			t.Fatalf("renewals before first interval = %d, want the initial renewal only", renewals)
		}
		select {
		case <-canceled:
			t.Fatal("checker canceled before lease renewal was lost")
		default:
		}
		synctest.Sleep(time.Millisecond)
		select {
		case <-canceled:
		default:
			t.Fatal("checker was not canceled when the next renewal lost the lease")
		}
		if err := <-done; !errors.Is(err, ErrLeaseLost) || !strings.HasPrefix(err.Error(), "checker.Worker.Handle - ") {
			t.Fatalf("Handle() error = %v, want ErrLeaseLost", err)
		}
		complete, accounted, renewals, _, _ := repo.snapshot()
		if complete != 0 || accounted != 0 || renewals < 2 {
			t.Fatalf("lease loss completed/accounted work: complete=%d accounted=%d renewals=%d", complete, accounted, renewals)
		}
	})
}

func TestExternalCancellationDoesNotCompleteClaimedJob(t *testing.T) {
	task := defaultTask()
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	started := make(chan struct{})
	runner := &fakeRunner{fn: func(ctx context.Context, _ processchecker.Request) (processchecker.Result, error) {
		close(started)
		<-ctx.Done()
		return processchecker.Result{}, context.Cause(ctx)
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Handle(ctx, testJobID) }()
	waitSignal(t, started, "checker start")
	cancel()
	if err := waitError(t, done); !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "checker.Worker.Handle - ") {
		t.Fatalf("Handle() error = %v, want context.Canceled", err)
	}
	complete, accounted, _, _, _ := repo.snapshot()
	if complete != 0 || accounted != 0 {
		t.Fatalf("canceled workflow completed/accounted: %d/%d", complete, accounted)
	}
}

func TestAbsoluteWorkflowTimeoutCompletesCheckFailedStatus110(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		task := defaultTask()
		task.Puts = 1
		repo := newFakeRepository(payloadJSON(t, "puts_only", task), domain.JobActionPutsOnly)
		started := make(chan struct{})
		canceled := make(chan struct{})
		runner := &fakeRunner{fn: func(ctx context.Context, _ processchecker.Request) (processchecker.Result, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return processchecker.Result{}, context.Cause(ctx)
		}}
		const workflowLimit = 100 * time.Millisecond
		const recoveryGrace = 80 * time.Millisecond
		worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{
			LeaseDuration: 30 * time.Millisecond, RenewInterval: 5 * time.Millisecond,
			RecoveryGrace: recoveryGrace, MaxWorkflowDuration: workflowLimit, WorkConcurrency: 1,
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- worker.Handle(ctx, testJobID) }()
		waitSignal(t, started, "checker action start")
		synctest.Sleep(workflowLimit - time.Millisecond)
		select {
		case <-canceled:
			t.Fatal("checker was canceled before the workflow deadline")
		default:
		}
		_, accountedBeforeDeadline, _, _, _ := repo.snapshot()
		if accountedBeforeDeadline != 0 {
			t.Fatalf("workflow accounted before deadline: %d", accountedBeforeDeadline)
		}
		synctest.Sleep(time.Millisecond)
		select {
		case <-canceled:
		default:
			t.Fatal("checker was not canceled at the workflow deadline")
		}
		if err := <-done; err != nil {
			t.Fatalf("Handle() at configured workflow timeout: %v", err)
		}
		_, accounted, _, _, results := repo.snapshot()
		if accounted != 1 || len(results) != 1 || results[0].Status != workflowTimeoutStatus || results[0].Status == int32(processchecker.StatusDown) {
			t.Fatalf("workflow timeout result = accounted %d, %#v; want CHECK_FAILED status 110", accounted, results)
		}
		claimAt, deadlines := repo.deadlineSnapshot()
		if len(deadlines) == 0 {
			t.Fatal("workflow timeout did not set a durable deadline")
		}
		maximum := claimAt.Add(workflowLimit + recoveryGrace)
		for _, deadline := range deadlines {
			if deadline.After(maximum.Add(time.Millisecond)) {
				t.Errorf("durable job deadline %s exceeded absolute recovery cutoff %s", deadline, maximum)
			}
		}
	})
}

func TestScheduledCheckerSurvivesTeamTokenRotationWhileTeamIsActive(t *testing.T) {
	task := defaultTask()
	payload := domain.JobPayload{
		Team: domain.Team{ID: testTeamID, Name: "red", IP: "192.0.2.5", Token: "rotated-token", Active: true},
		Task: task,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeRepository(data, domain.JobActionClassic)
	runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
		return success(req.Action), nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatalf("active team job with rotated payload token: %v", err)
	}
	requests, _ := runner.snapshot()
	if len(requests) != 1 || requests[0].Action != processchecker.ActionCheck {
		t.Fatalf("checker actions after token rotation = %#v", requests)
	}
}

func TestDuplicateDeliveryAcknowledgesOnlyKnownClosedWithoutReaccounting(t *testing.T) {
	task := defaultTask()
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
		return success(req.Action), nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatal(err)
	}
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatalf("redelivery after durable completion: %v", err)
	}
	complete, accounted, _, _, _ := repo.snapshot()
	requests, _ := runner.snapshot()
	if complete != 1 || accounted != 1 || len(requests) != 1 {
		t.Fatalf("duplicate delivery repeated work: completes=%d accounting=%d runs=%d", complete, accounted, len(requests))
	}
}

func TestUnknownCompleteOutcomeAcknowledgesDurableFinishWithoutDuplicateAccounting(t *testing.T) {
	task := defaultTask()
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	repo.completeUnknown = true
	runner := &fakeRunner{fn: func(_ context.Context, req processchecker.Request) (processchecker.Result, error) {
		return success(req.Action), nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatalf("Handle() after ambiguous but durable Complete: %v", err)
	}
	if err := worker.Handle(context.Background(), testJobID); err != nil {
		t.Fatalf("redelivery after durable Complete: %v", err)
	}
	complete, accounted, _, _, _ := repo.snapshot()
	requests, _ := runner.snapshot()
	if complete != 1 || accounted != 1 || len(requests) != 1 {
		t.Fatalf("ambiguous completion repeated work: completes=%d accounting=%d runs=%d", complete, accounted, len(requests))
	}
}

func TestActiveCompetingLeaseIsNotAcknowledged(t *testing.T) {
	task := defaultTask()
	repo := newFakeRepository(payloadJSON(t, "", task), domain.JobActionClassic)
	repo.claimErr = domain.ErrJobClaimed
	deadline := time.Now().Add(time.Minute)
	repo.setState(testJobID, domain.CheckerJobState{DeadlineAt: &deadline})
	runner := &fakeRunner{fn: func(context.Context, processchecker.Request) (processchecker.Result, error) {
		t.Fatal("checker ran despite active competing lease")
		return processchecker.Result{}, nil
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{})
	if err := worker.Handle(context.Background(), testJobID); !errors.Is(err, domain.ErrJobClaimed) {
		t.Fatalf("Handle() error = %v, want ErrJobClaimed for requeue", err)
	}
	complete, _, _, _, _ := repo.snapshot()
	if complete != 0 {
		t.Fatalf("completion calls = %d, want 0", complete)
	}
}

func TestWorkConcurrencyCapsActionsAcrossJobs(t *testing.T) {
	task := defaultTask()
	payload := payloadJSON(t, "", task)
	repo := newFakeRepository(payload, domain.JobActionClassic)
	repo.jobs[secondJobID] = domain.CheckerJob{ID: secondJobID, Round: 7, Action: domain.JobActionClassic, TeamID: testTeamID, TaskID: testTaskID, Payload: payload}
	started := make(chan string, 2)
	release := make(chan struct{})
	runner := &fakeRunner{fn: func(ctx context.Context, _ processchecker.Request) (processchecker.Result, error) {
		started <- "action"
		select {
		case <-release:
			return success(processchecker.ActionCheck), nil
		case <-ctx.Done():
			return processchecker.Result{}, context.Cause(ctx)
		}
	}}
	worker := makeWorker(t, repo, &fakeFlagStore{}, runner, Options{WorkConcurrency: 1})
	done := make(chan error, 2)
	go func() { done <- worker.Handle(context.Background(), testJobID) }()
	waitSignal(t, started, "first job action")
	go func() { done <- worker.Handle(context.Background(), secondJobID) }()
	select {
	case <-started:
		t.Fatal("second job entered runner while global action limit was occupied")
	case <-time.After(25 * time.Millisecond):
	}
	release <- struct{}{}
	waitSignal(t, started, "second job action")
	release <- struct{}{}
	if err := waitError(t, done); err != nil {
		t.Fatal(err)
	}
	if err := waitError(t, done); err != nil {
		t.Fatal(err)
	}
	_, maximum := runner.snapshot()
	if maximum != 1 {
		t.Fatalf("max concurrent checker actions = %d, want 1", maximum)
	}
}

func waitSignal[T any](t *testing.T, channel <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(2 * time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", what)
		return zero
	}
}

func waitValue[T any](t *testing.T, channel <-chan T, what string) T {
	t.Helper()
	return waitSignal(t, channel, what)
}

func waitError(t *testing.T, channel <-chan error) error {
	t.Helper()
	select {
	case err := <-channel:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for worker")
		return nil
	}
}

func int32ptr(value int32) *int32 { return &value }

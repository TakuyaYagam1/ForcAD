package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

var testJobID = uuid.MustParse("11111111-1111-4111-8111-111111111111")

func TestScheduleDue(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	last := at.Add(-time.Minute)
	tests := []struct {
		name     string
		lastRun  *time.Time
		at       time.Time
		interval time.Duration
		want     bool
	}{
		{name: "first run", at: at, interval: time.Minute, want: true},
		{name: "interval boundary", lastRun: &last, at: at, interval: time.Minute, want: true},
		{name: "before interval boundary", lastRun: &last, at: at.Add(-time.Nanosecond), interval: time.Minute, want: false},
		{name: "zero interval", lastRun: &last, at: at, interval: 0, want: true},
		{name: "negative interval", lastRun: &last, at: at, interval: -time.Second, want: false},
		{name: "zero time", at: time.Time{}, interval: time.Second, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scheduleDue(test.lastRun, test.at, test.interval); got != test.want {
				t.Fatalf("scheduleDue() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSchedulerTimeUsesPauseClock(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	pausedAt := at.Add(-10 * time.Second)
	life := domain.Lifecycle{Session: domain.Session{SchedulePausedSeconds: 30}}
	if got, want := schedulerTime(life, at), at.Add(-30*time.Second); !got.Equal(want) {
		t.Fatalf("schedulerTime() = %s, want %s", got, want)
	}
	life.Session.PausedAt = &pausedAt
	if got, want := schedulerTime(life, at), at.Add(-40*time.Second); !got.Equal(want) {
		t.Fatalf("schedulerTime() with active pause = %s, want %s", got, want)
	}
}

func TestEffectiveStartPrefersSessionStart(t *testing.T) {
	configStart := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	practiceStart := configStart.Add(-time.Hour)
	finalStart := configStart.Add(-2 * time.Hour)
	tests := []struct {
		name string
		life domain.Lifecycle
		want time.Time
	}{
		{name: "configured start", life: domain.Lifecycle{Config: domain.GameConfig{StartTime: configStart}}, want: configStart},
		{name: "practice start", life: domain.Lifecycle{Config: domain.GameConfig{StartTime: configStart}, Session: domain.Session{PracticeStart: &practiceStart}}, want: practiceStart},
		{name: "final start wins", life: domain.Lifecycle{Config: domain.GameConfig{StartTime: configStart}, Session: domain.Session{PracticeStart: &practiceStart, FinalStart: &finalStart}}, want: finalStart},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := effectiveStart(test.life); !got.Equal(test.want) {
				t.Fatalf("effectiveStart() = %s, want %s", got, test.want)
			}
		})
	}
}

type taskListingRepository struct {
	outbound.GameRepository

	tasks []domain.Task
}

func (r taskListingRepository) Tasks(context.Context, bool) ([]domain.Task, error) {
	return r.tasks, nil
}

func TestBlitzTaskScheduleIDPreservesFullUUID(t *testing.T) {
	taskID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	life := domain.Lifecycle{Config: domain.GameConfig{Mode: "blitz", RoundTime: 60}}
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	requests, err := scheduleRequests(context.Background(), taskListingRepository{
		tasks: []domain.Task{{ID: taskID, GetPeriod: 15}},
	}, life, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("scheduleRequests() returned %d requests, want round and task schedules", len(requests))
	}
	request := requests[1]
	wantID := "blitz_check_gets_task_" + taskID.String()
	if request.ScheduleID != wantID || len(request.ScheduleID) <= 32 {
		t.Fatalf("blitz task schedule ID = %q, want full UUID schedule ID %q", request.ScheduleID, wantID)
	}
	if request.TaskID == nil || *request.TaskID != taskID {
		t.Fatalf("blitz task ID = %v, want %s", request.TaskID, taskID)
	}
}

func TestBlitzScheduleRejectsNilTaskID(t *testing.T) {
	life := domain.Lifecycle{Config: domain.GameConfig{Mode: "blitz", RoundTime: 60}}
	_, err := scheduleRequests(context.Background(), taskListingRepository{
		tasks: []domain.Task{{GetPeriod: 15}},
	}, life, time.Now(), time.Now())
	if err == nil || !strings.HasPrefix(err.Error(), "scheduler.scheduleRequests - ") {
		t.Fatalf("scheduleRequests() error = %v, want located nil task ID error", err)
	}
}

func TestNewCapsBatchAndSetsDefaults(t *testing.T) {
	scheduler := New(nil, nil, nil, nil, Options{BatchSize: 1000})
	if scheduler.options.BatchSize != maxPublishBatch {
		t.Fatalf("batch size = %d, want %d", scheduler.options.BatchSize, maxPublishBatch)
	}
	if scheduler.options.PollInterval <= 0 || scheduler.options.OutboxLease <= 0 ||
		scheduler.options.RecoveryGrace != defaultRecoveryGrace || scheduler.options.RecoveryLimit <= 0 {
		t.Fatalf("default options are incomplete: %+v", scheduler.options)
	}
}

func TestNewUsesConfiguredRecoveryGrace(t *testing.T) {
	grace := 47 * time.Second
	scheduler := New(nil, nil, nil, nil, Options{RecoveryGrace: grace})
	if scheduler.options.RecoveryGrace != grace {
		t.Fatalf("recovery grace = %s, want configured %s", scheduler.options.RecoveryGrace, grace)
	}
}

func TestInitialJobDeadlineUsesCheckerTimeoutAndGrace(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	job := domain.CheckerJob{
		ID:      testJobID,
		Payload: json.RawMessage(`{"task":{"checker_timeout":20}}`),
	}
	deadline, err := initialJobDeadline(job, at, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := at.Add(50 * time.Second); !deadline.Equal(want) {
		t.Fatalf("initial deadline = %s, want %s", deadline, want)
	}
}

func TestInitialJobDeadlineRejectsNilJobID(t *testing.T) {
	job := domain.CheckerJob{
		Payload: json.RawMessage(`{"task":{"checker_timeout":20}}`),
	}
	if _, err := initialJobDeadline(job, time.Now(), 30*time.Second); err == nil || !strings.HasPrefix(err.Error(), "scheduler.initialJobDeadline - ") {
		t.Fatalf("initialJobDeadline() error = %v, want located nil-ID error", err)
	}
}

func TestInitialJobDeadlineAcceptsCanonicalFieldsAndUnknownFields(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	job := domain.CheckerJob{
		ID:      testJobID,
		Payload: json.RawMessage(`{"task":{"checker_timeout":20,"future_task_field":true},"future_root_field":true}`),
	}
	deadline, err := initialJobDeadline(job, at, 30*time.Second)
	if err != nil {
		t.Fatalf("initialJobDeadline() rejected canonical fields with unknown fields: %v", err)
	}
	if want := at.Add(50 * time.Second); !deadline.Equal(want) {
		t.Fatalf("initial deadline = %s, want %s", deadline, want)
	}
}

func TestInitialJobDeadlineRequiresCanonicalCase(t *testing.T) {
	job := domain.CheckerJob{
		ID:      testJobID,
		Payload: json.RawMessage(`{"TASK":{"CHECKER_TIMEOUT":20}}`),
	}
	if _, err := initialJobDeadline(job, time.Now(), 30*time.Second); err == nil {
		t.Fatal("initialJobDeadline() accepted legacy case variants")
	}
}

func TestInitialJobDeadlineRejectsDuplicateNamesAndInvalidUTF8(t *testing.T) {
	invalidUTF8 := []byte(`{"task":{"checker_timeout":20,"name":"`)
	invalidUTF8 = append(invalidUTF8, 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}}`)...)
	for _, test := range []struct {
		name    string
		payload []byte
	}{
		{name: "duplicate name", payload: []byte(`{"task":{"checker_timeout":20,"checker_timeout":30}}`)},
		{name: "invalid UTF-8", payload: invalidUTF8},
	} {
		t.Run(test.name, func(t *testing.T) {
			job := domain.CheckerJob{ID: testJobID, Payload: test.payload}
			if _, err := initialJobDeadline(job, time.Now(), 30*time.Second); err == nil {
				t.Fatal("initialJobDeadline() accepted malformed job payload")
			}
		})
	}
}

type malformedPayloadRepository struct {
	Repository

	job          domain.CheckerJob
	markedAsSent int
}

func (r *malformedPayloadRepository) ClaimPending(context.Context, string, time.Time, time.Time, int32) ([]domain.CheckerJob, error) {
	return []domain.CheckerJob{r.job}, nil
}

func (r *malformedPayloadRepository) MarkPublished(context.Context, uuid.UUID, string, time.Time, time.Time) (bool, error) {
	r.markedAsSent++
	return true, nil
}

type confirmingPublisher struct{ confirmed []uuid.UUID }

func (p *confirmingPublisher) Publish(_ context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	p.confirmed = append([]uuid.UUID(nil), ids...)
	return append([]uuid.UUID(nil), ids...), nil
}

func TestMalformedPublishedPayloadGetsNoDeadlineCheckpoint(t *testing.T) {
	repository := &malformedPayloadRepository{
		job: domain.CheckerJob{ID: testJobID, Payload: []byte(`{"task":{"checker_timeout":20,"checker_timeout":30}}`)},
	}
	publisher := &confirmingPublisher{}
	scheduler := &Scheduler{
		repository: repository,
		publisher:  publisher,
		options:    Options{BatchSize: 1, RecoveryGrace: 30 * time.Second},
	}
	if err := scheduler.publishBatch(context.Background(), time.Now()); err == nil {
		t.Fatal("publishBatch() accepted a payload without an unambiguous deadline")
	}
	if len(publisher.confirmed) != 1 || publisher.confirmed[0] != repository.job.ID {
		t.Fatalf("publisher confirmations = %v, want the malformed durable job", publisher.confirmed)
	}
	if repository.markedAsSent != 0 {
		t.Fatalf("malformed job received %d publication checkpoints, want none", repository.markedAsSent)
	}
}

type checkpointRepository struct{ Repository }

func (checkpointRepository) LastRun(ctx context.Context, _ string) (*time.Time, error) {
	return nil, ctx.Err()
}

func TestDuePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Scheduler{repository: checkpointRepository{}}
	_, err := s.due(ctx, domain.ScheduleRequest{ScheduleID: "classic_rounds"}, time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("due() error = %v, want cancellation", err)
	}
	if !strings.HasPrefix(err.Error(), "scheduler.Scheduler.due - ") {
		t.Fatalf("error is missing its source: %v", err)
	}
}

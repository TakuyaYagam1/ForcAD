package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type extraCheckFixture struct {
	pool       *pgxpool.Pool
	repository *postgres.Repository
	start      time.Time
	run        *domain.DispatchRun
	request    domain.ScheduleRequest
}

func newExtraCheckFixture(t *testing.T) extraCheckFixture {
	t.Helper()
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	document := scheduledGameDocument(t)
	document.Game.ExtraChecks = 2
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, document); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil || life.Config.ExtraChecks != 2 {
		t.Fatalf("configured extra checks: %+v, %v", life.Config, err)
	}
	start := life.Config.StartTime
	repository := postgres.New(pool)
	if started, err := repository.StartScheduled(ctx, start); err != nil || !started {
		t.Fatalf("start: %v, %v", started, err)
	}
	request := domain.ScheduleRequest{
		ScheduleID: "classic_rounds", At: start, StartAt: start,
		Interval: time.Minute, AdvancesRound: true, Workflow: domain.JobActionClassic,
	}
	run, err := repository.Prepare(ctx, request)
	if err != nil || run == nil {
		t.Fatalf("prepare: %v, %v", run, err)
	}
	return extraCheckFixture{pool, repository, start, run, request}
}

func (f extraCheckFixture) claim(t *testing.T, at time.Time, expected int) []domain.CheckerJob {
	t.Helper()
	ctx := context.Background()
	jobs, err := f.repository.ClaimPending(ctx, "publisher", at, at.Add(time.Minute), 20)
	if err != nil || len(jobs) != expected {
		t.Fatalf("pending jobs = %d, %v; want %d", len(jobs), err, expected)
	}
	for i, job := range jobs {
		if ok, err := f.repository.MarkPublished(ctx, job.ID, "publisher", at, at.Add(time.Minute)); err != nil || !ok {
			t.Fatalf("publish = %t, %v", ok, err)
		}
		jobs[i], err = f.repository.Claim(ctx, job.ID, "worker", at, at.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	return jobs
}

func (f extraCheckFixture) complete(t *testing.T, job domain.CheckerJob, at time.Time, status int32) {
	t.Helper()
	ok, err := f.repository.Complete(context.Background(), job.ID, "worker", at,
		domain.CheckerResult{Round: job.Round, Status: status})
	if err != nil || !ok {
		t.Fatalf("complete = %t, %v", ok, err)
	}
}

func (f extraCheckFixture) assertScore(t *testing.T, status, checks, passed int32) {
	t.Helper()
	var actualStatus, actualChecks, actualPassed int32
	if err := f.pool.QueryRow(context.Background(), `SELECT status, checks, checks_passed FROM TeamTasks`).
		Scan(&actualStatus, &actualChecks, &actualPassed); err != nil {
		t.Fatal(err)
	}
	if actualStatus != status || actualChecks != checks || actualPassed != passed {
		t.Fatalf("score = %d/%d/%d; want %d/%d/%d", actualStatus, actualChecks, actualPassed, status, checks, passed)
	}
}

func TestExtraChecksPersistTimesAndLatchDown(t *testing.T) {
	f := newExtraCheckFixture(t)
	ctx := context.Background()
	var before string
	query := `SELECT json_agg(json_build_array(id, not_before, expires_at) ORDER BY id)::text FROM CheckerJobs`
	if err := f.pool.QueryRow(ctx, query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// A new repository represents a restarted ticker. Prepare must reuse IDs/times.
	f.repository = postgres.New(f.pool)
	run, err := f.repository.Prepare(ctx, f.request)
	var after string
	if err != nil || run.ID != f.run.ID {
		t.Fatalf("prepare after restart: %v, %v", run, err)
	}
	if err := f.pool.QueryRow(ctx, query).Scan(&after); err != nil || before != after {
		t.Fatalf("schedule changed after restart: %v", err)
	}
	main := f.claim(t, f.start, 1)[0]
	f.complete(t, main, f.start.Add(time.Second), 104)
	var checks int
	if err := f.pool.QueryRow(ctx, `SELECT checks FROM TeamTasks`).Scan(&checks); err != nil || checks != 0 {
		t.Fatalf("unfinished group affected SLA: %d, %v", checks, err)
	}
	extras := f.claim(t, f.start.Add(49*time.Second), 2)
	for _, job := range extras {
		if job.Action != domain.JobActionCheckOnly || job.ExpiresAt == nil ||
			!job.ExpiresAt.Equal(f.start.Add(time.Minute)) {
			t.Fatalf("extra job: %+v", job)
		}
		f.complete(t, job, f.start.Add(50*time.Second), domain.TaskStatusUp)
	}
	f.assertScore(t, 104, 1, 0)
}

func TestExtraChecksCompleteConcurrentlyOnlyOnce(t *testing.T) {
	f := newExtraCheckFixture(t)
	at := f.start.Add(49 * time.Second)
	jobs := f.claim(t, at, 3)
	var wait sync.WaitGroup
	errorsFound := make(chan error, len(jobs))
	for _, job := range jobs {
		wait.Go(func() {
			ok, err := f.repository.Complete(context.Background(), job.ID, "worker", at.Add(time.Second),
				domain.CheckerResult{Round: 1, Status: domain.TaskStatusUp})
			if err != nil {
				errorsFound <- err
			} else if !ok {
				errorsFound <- errors.New("concurrent result rejected")
			}
		})
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	f.assertScore(t, domain.TaskStatusUp, 1, 1)
	for _, job := range jobs {
		ok, err := f.repository.Complete(context.Background(), job.ID, "worker", at.Add(2*time.Second),
			domain.CheckerResult{Round: 1, Status: domain.TaskStatusUp})
		if err != nil || ok {
			t.Fatalf("duplicate completion = %t, %v", ok, err)
		}
	}
	f.assertScore(t, domain.TaskStatusUp, 1, 1)
}

func TestExtraChecksPauseShiftsUnfinishedJobs(t *testing.T) {
	f := newExtraCheckFixture(t)
	ctx := context.Background()
	main := f.claim(t, f.start, 1)[0]
	f.complete(t, main, f.start.Add(100*time.Millisecond), domain.TaskStatusUp)
	pause := f.start.Add(500 * time.Millisecond)
	game := postgres.NewGameStore(f.pool)
	if err := game.ChangeState(ctx, "pause", nil, pause); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	var notBefore, expires time.Time
	if err := f.pool.QueryRow(ctx, `SELECT id, not_before, expires_at FROM CheckerJobs
		WHERE action = 'check_only' ORDER BY id LIMIT 1`).Scan(&id, &notBefore, &expires); err != nil {
		t.Fatal(err)
	}
	f.claim(t, f.start.Add(2*time.Minute), 0)
	if ok, err := f.repository.Expire(ctx, id, f.start.Add(2*time.Minute)); err != nil || ok {
		t.Fatalf("paused job expired = %t, %v", ok, err)
	}
	resume := pause.Add(2 * time.Minute)
	if err := game.ChangeState(ctx, "resume", nil, resume); err != nil {
		t.Fatal(err)
	}
	var shiftedStart, shiftedEnd time.Time
	if err := f.pool.QueryRow(ctx, `SELECT not_before, expires_at FROM CheckerJobs WHERE id = $1`, id).
		Scan(&shiftedStart, &shiftedEnd); err != nil {
		t.Fatal(err)
	}
	if !shiftedStart.Equal(notBefore.Add(2*time.Minute)) || !shiftedEnd.Equal(expires.Add(2*time.Minute)) {
		t.Fatalf("incorrect pause shift: %v/%v", shiftedStart, shiftedEnd)
	}
	for _, job := range f.claim(t, f.start.Add(169*time.Second), 2) {
		f.complete(t, job, f.start.Add(170*time.Second), domain.TaskStatusUp)
	}
	f.assertScore(t, domain.TaskStatusUp, 1, 1)
}

func TestExtraChecksQueueExpiryDoesNotPenalizeSLA(t *testing.T) {
	f := newExtraCheckFixture(t)
	main := f.claim(t, f.start, 1)[0]
	f.complete(t, main, f.start.Add(time.Second), domain.TaskStatusUp)
	rows, err := f.pool.Query(context.Background(), `SELECT id FROM CheckerJobs WHERE action = 'check_only'`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if ok, err := f.repository.Expire(context.Background(), id, f.start.Add(time.Minute)); err != nil || !ok {
			t.Fatalf("expire = %t, %v", ok, err)
		}
	}
	f.assertScore(t, domain.TaskStatusCheckFailed, 0, 0)
}

func TestExtraChecksCanBeReclaimedAfterWorkerRestart(t *testing.T) {
	f := newExtraCheckFixture(t)
	ctx := context.Background()
	main := f.claim(t, f.start, 1)[0]
	f.complete(t, main, f.start.Add(time.Second), domain.TaskStatusUp)
	at := f.start.Add(49 * time.Second)
	jobs := f.claim(t, at, 2)
	for _, job := range jobs {
		if ok, err := f.repository.SetDeadline(ctx, job.ID, "worker", at, at.Add(100*time.Second)); err != nil || !ok {
			t.Fatalf("extend = %t, %v", ok, err)
		}
		// Simulate a worker lost after claiming; a new worker can repeat CHECK safely.
		if _, err := f.pool.Exec(ctx, `UPDATE CheckerJobs SET claim_deadline_at=$2 WHERE id=$1`,
			job.ID, at.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		restarted := postgres.New(f.pool)
		claimed, err := restarted.Claim(ctx, job.ID, "replacement", at.Add(2*time.Second), at.Add(30*time.Second))
		if err != nil || claimed.ExpiresAt == nil || !claimed.ExpiresAt.Equal(f.start.Add(time.Minute)) {
			t.Fatalf("reclaim = %+v, %v", claimed, err)
		}
		if ok, err := restarted.Complete(ctx, job.ID, "worker", at.Add(3*time.Second),
			domain.CheckerResult{Round: 1, Status: 104}); err != nil || ok {
			t.Fatalf("stale worker completion = %t, %v", ok, err)
		}
		if ok, err := restarted.Complete(ctx, job.ID, "replacement", at.Add(3*time.Second),
			domain.CheckerResult{Round: 1, Status: domain.TaskStatusUp}); err != nil || !ok {
			t.Fatalf("replacement completion = %t, %v", ok, err)
		}
	}
	f.assertScore(t, domain.TaskStatusUp, 1, 1)
}

func TestExtraChecksRejectTaskTimeoutThatWouldBlockScheduling(t *testing.T) {
	f := newExtraCheckFixture(t)
	ctx := context.Background()
	game := postgres.NewGameStore(f.pool)
	tasks, err := game.Tasks(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	task.CheckerTimeout = 57
	for _, save := range []func(context.Context, domain.Task) (domain.Task, error){game.CreateTask, game.UpdateTask} {
		_, err := save(ctx, task)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("unsafe timeout accepted: %v", err)
		}
	}
	task.CheckerTimeout = 10
	if _, err := game.UpdateTask(ctx, task); err != nil {
		t.Fatalf("valid timeout rejected: %v", err)
	}
}

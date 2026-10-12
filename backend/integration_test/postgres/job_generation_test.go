package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestCheckerJobGenerationFence(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	repository := postgres.New(pool)
	if started, err := repository.StartScheduled(ctx, start); err != nil || !started {
		t.Fatalf("scheduled start = %t, %v; want true", started, err)
	}
	life, err = game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if life.Session.Generation != 1 {
		t.Fatalf("scheduled generation = %d, want 1", life.Session.Generation)
	}
	run, err := repository.Prepare(ctx, domain.ScheduleRequest{
		ScheduleID: "generation_fence", At: start, StartAt: start,
		Interval: time.Minute, AdvancesRound: true, Workflow: domain.JobActionClassic,
	})
	if err != nil || run == nil {
		t.Fatalf("prepare scheduled job = %+v, %v", run, err)
	}
	var currentID uuid.UUID
	var generation int64
	if err := pool.QueryRow(ctx, `SELECT id, (payload->>'generation')::BIGINT
		FROM CheckerJobs WHERE run_id=$1`, run.ID).Scan(&currentID, &generation); err != nil {
		t.Fatal(err)
	}
	if generation != life.Session.Generation {
		t.Fatalf("scheduler emitted generation %d, want %d", generation, life.Session.Generation)
	}
	legacyID, staleID := uuid.NewV7(), uuid.NewV7()
	for _, id := range []uuid.UUID{legacyID, staleID} {
		if _, err := pool.Exec(ctx, `INSERT INTO CheckerJobs (id,run_id,round,payload,action,team_id,task_id)
			SELECT $1,run_id,round,payload,action,team_id,task_id FROM CheckerJobs WHERE id=$2`, id, currentID); err != nil {
			t.Fatal(err)
		}
	}

	jobTime := start.Add(time.Second)
	publishClaims, err := repository.ClaimPending(ctx, "publisher", jobTime, jobTime.Add(time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	publishTokens := make(map[uuid.UUID]string, len(publishClaims))
	for _, job := range publishClaims {
		if job.PublishToken == nil {
			t.Fatalf("job %q has no outbox claim token", job.ID)
		}
		publishTokens[job.ID] = *job.PublishToken
	}
	if len(publishTokens) != 3 {
		t.Fatalf("initial outbox claims = %d, want all three generation-1 jobs", len(publishTokens))
	}
	if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET payload=payload-'generation' WHERE id=$1`, legacyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET payload=jsonb_set(payload,'{generation}','0'::JSONB)
		WHERE id=$1`, staleID); err != nil {
		t.Fatal(err)
	}

	publishedAt := jobTime.Add(time.Second)
	deadline := jobTime.Add(time.Hour)
	if ok, err := repository.MarkPublished(ctx, currentID, publishTokens[currentID], publishedAt, deadline); err != nil || !ok {
		t.Fatalf("publish current-generation job = %t, %v; want true", ok, err)
	}
	for _, id := range []uuid.UUID{legacyID, staleID} {
		if ok, err := repository.MarkPublished(ctx, id, publishTokens[id], publishedAt, deadline); err != nil || ok {
			t.Fatalf("publish fenced job %q = %t, %v; want false", id, ok, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET publish_token=NULL,publish_deadline_at=NULL
		WHERE id=ANY($1::UUID[])`, []uuid.UUID{legacyID, staleID}); err != nil {
		t.Fatal(err)
	}
	remaining, err := repository.ClaimPending(ctx, "publisher-retry", publishedAt, deadline, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("outbox reclaimed %d generationless or stale jobs", len(remaining))
	}

	for _, id := range []uuid.UUID{legacyID, staleID} {
		if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET sent_at=$2,deadline_at=$3,
			publish_token=NULL,publish_deadline_at=NULL WHERE id=$1`, id, publishedAt, deadline); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.Claim(ctx, id, "claim-attempt", publishedAt.Add(time.Second), publishedAt.Add(time.Minute)); !errors.Is(err, domain.ErrJobNotReady) {
			t.Fatalf("claim fenced job %q error = %v, want not-ready", id, err)
		}
	}

	current, err := repository.Claim(ctx, currentID, "current-owner", publishedAt.Add(time.Second), publishedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim current-generation job: %v", err)
	}
	if current.ClaimToken == nil {
		t.Fatal("claimed current-generation job has no owner token")
	}
	if _, err := repository.Create(ctx, domain.Flag{
		Value: "current-generation-flag", TeamID: current.TeamID, TaskID: current.TaskID, Round: current.Round,
	}, &domain.CheckerJobOwner{JobID: current.ID, ClaimToken: *current.ClaimToken}); err != nil {
		t.Fatalf("commit flag for current-generation job: %v", err)
	}
	completedAt := publishedAt.Add(2 * time.Second)
	if completed, err := repository.Complete(ctx, currentID, *current.ClaimToken, completedAt, domain.CheckerResult{
		Round: current.Round, Status: domain.TaskStatusUp,
	}); err != nil || !completed {
		t.Fatalf("complete current-generation job = %t, %v; want true", completed, err)
	}

	for _, id := range []uuid.UUID{legacyID, staleID} {
		owner := "owner-" + id.String()
		if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET claim_token=$2,claim_deadline_at=$3 WHERE id=$1`,
			id, owner, deadline); err != nil {
			t.Fatal(err)
		}
		flag, err := repository.Create(ctx, domain.Flag{
			Value: "fenced-" + id.String(), TeamID: current.TeamID, TaskID: current.TaskID, Round: current.Round,
		}, &domain.CheckerJobOwner{JobID: id, ClaimToken: owner})
		if !errors.Is(err, domain.ErrCheckerJobClosed) {
			t.Fatalf("commit flag for fenced job %q returned flag %d, error %v", id, flag.ID, err)
		}
		if renewed, err := repository.Renew(ctx, id, owner, publishedAt.Add(3*time.Second), deadline); err != nil || renewed {
			t.Fatalf("renew fenced job %q = %t, %v; want false", id, renewed, err)
		}
		if completed, err := repository.Complete(ctx, id, owner, publishedAt.Add(4*time.Second), domain.CheckerResult{
			Round: current.Round, Status: domain.TaskStatusUp,
		}); err != nil || completed {
			t.Fatalf("complete fenced job %q = %t, %v; want false", id, completed, err)
		}
	}

	var flagCount, resultCount, logCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM Flags WHERE flag IN ('current-generation-flag','fenced-job-missing-generation','fenced-job-stale-generation')),
		(SELECT count(*) FROM TeamTasks WHERE team_id=$1 AND task_id=$2 AND status=$3 AND checks=1 AND checks_passed=1),
		(SELECT count(*) FROM TeamTasksLog WHERE team_id=$1 AND task_id=$2 AND round=$4)`,
		current.TeamID, current.TaskID, domain.TaskStatusUp, current.Round).Scan(&flagCount, &resultCount, &logCount); err != nil {
		t.Fatal(err)
	}
	if flagCount != 1 || resultCount != 1 || logCount != 1 {
		t.Fatalf("committed flags/results/logs = %d/%d/%d, want one current-generation commit", flagCount, resultCount, logCount)
	}
}

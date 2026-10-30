package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestOutboxDispatchWhileLifecycleReadersAreActive(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo := postgres.New(pool)
	at := life.Config.StartTime
	if ok, err := repo.StartScheduled(ctx, at); err != nil || !ok {
		t.Fatalf("start = %v, %v", ok, err)
	}
	if run, err := repo.Prepare(ctx, domain.ScheduleRequest{ScheduleID: "outbox_shared_lifecycle", At: at, StartAt: at, Interval: time.Minute, AdvancesRound: true, Workflow: domain.JobActionClassic}); err != nil || run == nil {
		t.Fatalf("prepare = %v, %v", run, err)
	}
	reader, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Rollback(context.Background()) }()
	// This is the ordinary lock held by flag creation and submission transactions.
	if _, err := reader.Exec(ctx, `SELECT id FROM GameSession WHERE id=1 FOR SHARE`); err != nil {
		t.Fatal(err)
	}
	jobs, err := repo.ClaimPending(ctx, "publisher", at.Add(time.Second), at.Add(time.Minute), 100)
	if err != nil || len(jobs) == 0 {
		t.Fatalf("dispatch under compatible lifecycle reader = %d jobs, %v", len(jobs), err)
	}
	other, err := repo.ClaimPending(ctx, "other-publisher", at.Add(time.Second), at.Add(time.Minute), 100)
	if err != nil || len(other) != 0 {
		t.Fatalf("claimed jobs must remain exclusive: %d, %v", len(other), err)
	}
}

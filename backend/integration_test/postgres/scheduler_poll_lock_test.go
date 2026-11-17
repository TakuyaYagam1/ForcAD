package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
)

func TestIdleSchedulerPollsDoNotWaitForSubmissionLocks(t *testing.T) {
	for _, state := range []struct {
		name    string
		running bool
	}{
		{
			name:    "running",
			running: true,
		},
		{
			name:    "finished with completed snapshot",
			running: false,
		},
	} {
		t.Run(state.name, func(t *testing.T) {
			pool, _ := testutil.Database(t)
			game := postgres.NewGameStore(pool)
			if _, err := game.Initialize(t.Context(), scheduledGameDocument(t)); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(t.Context(), `UPDATE GameConfig SET real_round=1, game_running=$1 WHERE id=1`, state.running); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(t.Context(), `UPDATE GameSession SET final_start=now(), snapshot_round=1, snapshot='{}'::JSONB WHERE id=1`); err != nil {
				t.Fatal(err)
			}

			// An overlapping submission keeps these shares while waiting for score
			// cells. A poll with no transition to apply must leave it alone.
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			for _, query := range []string{
				`SELECT id FROM GameConfig WHERE id=1 FOR SHARE`,
				`SELECT id FROM GameSession WHERE id=1 FOR SHARE`,
			} {
				if _, err := tx.Exec(t.Context(), query); err != nil {
					t.Fatal(err)
				}
			}

			repository := postgres.New(pool)
			for _, poll := range []struct {
				name string
				call func(context.Context, time.Time) (bool, error)
			}{
				{
					name: "scheduled start",
					call: repository.StartScheduled,
				},
				{
					name: "maintenance",
					call: repository.Maintain,
				},
			} {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				changed, err := poll.call(ctx, time.Now())
				cancel()
				if err != nil || changed {
					t.Fatalf("idle %s behind submission locks = %t, %v; want false, nil", poll.name, changed, err)
				}
			}
		})
	}
}

func TestScheduledStartRechecksEligibilityAfterWaitingForStateLock(t *testing.T) {
	pool, _ := testutil.Database(t)
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(t.Context(), scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	var blockerPID int32
	if err := tx.QueryRow(t.Context(), `SELECT pg_backend_pid() FROM GameConfig WHERE id=1 FOR UPDATE`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		started bool
		err     error
	}
	result := make(chan outcome, 1)
	go func() {
		started, err := postgres.New(pool).StartScheduled(ctx, start)
		result <- outcome{
			started: started,
			err:     err,
		}
	}()

	// The committed state says start is due. Wait until the actual transition
	// requests the lock, then postpone the start in the concurrent transaction.
	waitForSchedulerStateLock(t, pool, blockerPID)
	postponed := start.Add(time.Hour)
	if _, err := tx.Exec(t.Context(), `UPDATE GameConfig SET start_time=$1 WHERE id=1`, postponed); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.started {
			t.Fatalf("postponed scheduled start = %t, %v; want false, nil", got.started, got.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	life, err = game.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if life.Config.GameRunning || !life.Config.StartTime.Equal(postponed) || life.Session.FinalStart != nil || life.Session.Generation != 0 {
		t.Fatalf("stale eligibility changed postponed session: %+v", life)
	}
}

func TestSchedulerMaintenanceRechecksSnapshotAfterWaitingForStateLock(t *testing.T) {
	pool, _ := testutil.Database(t)
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(t.Context(), scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE GameConfig SET real_round=1, game_running=FALSE WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE GameSession SET final_start=now(), snapshot_round=1, snapshot=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	var blockerPID int32
	if err := tx.QueryRow(t.Context(), `SELECT pg_backend_pid() FROM GameConfig WHERE id=1 FOR UPDATE`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		maintained bool
		err        error
	}
	result := make(chan outcome, 1)
	go func() {
		maintained, err := postgres.New(pool).Maintain(ctx, time.Now())
		result <- outcome{
			maintained: maintained,
			err:        err,
		}
	}()

	// A matching snapshot_round alone is insufficient while the snapshot is
	// SQL NULL. Another maintainer can fill it before we acquire the state lock.
	waitForSchedulerStateLock(t, pool, blockerPID)
	if _, err := tx.Exec(t.Context(), `UPDATE GameSession SET snapshot_round=1,
		snapshot='{"round":1,"round_start":0,"team_tasks":[],"marker":"concurrent"}'::JSONB WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.maintained {
			t.Fatalf("already snapshotted maintenance = %t, %v; want false, nil", got.maintained, got.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var round, storedSnapshots int32
	var marker string
	if err := pool.QueryRow(t.Context(), `SELECT snapshot_round, snapshot->>'marker',
		(SELECT count(*)::INTEGER FROM GameRoundSnapshots) FROM GameSession WHERE id=1`).
		Scan(&round, &marker, &storedSnapshots); err != nil {
		t.Fatal(err)
	}
	if round != 1 || marker != "concurrent" || storedSnapshots != 0 {
		t.Fatalf("concurrent snapshot was overwritten: round=%d marker=%q stored snapshots=%d", round, marker, storedSnapshots)
	}
}

func waitForSchedulerStateLock(t *testing.T, pool *pgxpool.Pool, blockerPID int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE $1::INTEGER = ANY(pg_blocking_pids(pid))
			  AND query LIKE '-- name: LockSchedulerGame%'
		)`, blockerPID).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("scheduled transition did not wait for concurrent state lock")
		case <-ticker.C:
		}
	}
}

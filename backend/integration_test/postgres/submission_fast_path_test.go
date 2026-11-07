package postgres_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type staleSubmissionReadKey struct{}

// Pause only B after its flag rows drain, before it requests score locks.
type staleSubmissionTrace struct {
	readDone                        chan struct{}
	resume                          chan struct{}
	readOnce, resumeOnce            sync.Once
	fast, fallback, legacy, commits atomic.Int64
}

func (trace *staleSubmissionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	sql := strings.TrimSpace(data.SQL)
	switch {
	case strings.HasPrefix(sql, "-- name: InsertSubmissionChanges "):
		trace.fast.Add(1)
	case strings.HasPrefix(sql, "-- name: ApplySubmissionChanges "):
		trace.fallback.Add(1)
	case strings.HasPrefix(sql, "-- name: InsertStolenFlags "):
		trace.legacy.Add(1)
	case sql == "commit":
		trace.commits.Add(1)
	}
	return context.WithValue(ctx, staleSubmissionReadKey{}, strings.HasPrefix(sql, "-- name: ReadSubmissionFlags "))
}

func (trace *staleSubmissionTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(staleSubmissionReadKey{}) != true || data.Err != nil {
		return
	}
	trace.readOnce.Do(func() {
		close(trace.readDone)
		select {
		case <-trace.resume:
		case <-ctx.Done():
		}
	})
}

func (trace *staleSubmissionTrace) release() {
	trace.resumeOnce.Do(func() {
		close(trace.resume)
	})
}

type staleSubmissionResult struct {
	results []domain.SubmissionResult
	err     error
}

func submitWithStaleFlagRead(
	t *testing.T,
	f submissionFixture,
	values []string,
	whilePaused func(),
) ([]domain.SubmissionResult, *staleSubmissionTrace, error) {
	t.Helper()
	trace := &staleSubmissionTrace{
		readDone: make(chan struct{}),
		resume:   make(chan struct{}),
	}
	settings := f.pool.Config()
	settings.ConnConfig.Tracer = trace
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer pool.Close()
	defer cancel()
	defer trace.release()
	done := make(chan staleSubmissionResult, 1)
	request := f.request(0, values)
	go func() {
		results, err := postgres.New(pool).SubmitBatch(ctx, request)
		done <- staleSubmissionResult{
			results: results,
			err:     err,
		}
	}()
	select {
	case <-trace.readDone:
	case result := <-done:
		t.Fatalf("B returned before its stale-read barrier: %+v, %v", result.results, result.err)
	case <-ctx.Done():
		t.Fatalf("B did not drain ReadSubmissionFlags: %v", ctx.Err())
	}
	whilePaused()
	trace.release()
	select {
	case result := <-done:
		return result.results, trace, result.err
	case <-ctx.Done():
		t.Fatalf("B did not finish after stale-read release: %v", ctx.Err())
		return nil, trace, ctx.Err()
	}
}

func seedAsymmetricSubmissionScores(t *testing.T, f submissionFixture) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), `UPDATE TeamTasks SET score=CASE WHEN team_id=$1 THEN 1200 ELSE 3800 END`, f.teams[0]); err != nil {
		t.Fatal(err)
	}
}

func assertSubmissionCell(
	t *testing.T,
	f submissionFixture,
	team int,
	score float64,
	stolen, lost int,
) {
	t.Helper()
	var actualScore float64
	var actualStolen, actualLost int
	if err := f.pool.QueryRow(t.Context(), `SELECT score,stolen,lost FROM TeamTasks WHERE team_id=$1 AND task_id=$2`, f.teams[team], f.task).Scan(&actualScore, &actualStolen, &actualLost); err != nil {
		t.Fatal(err)
	}
	if actualScore != score || actualStolen != stolen || actualLost != lost {
		t.Fatalf("team %d: score/stolen/lost=%v/%d/%d, want %v/%d/%d", team, actualScore, actualStolen, actualLost, score, stolen, lost)
	}
}

func TestFastSubmissionStaleReadPartialConflictRecomputesOriginalScores(t *testing.T) {
	f := newSubmissionFixture(t)
	seedAsymmetricSubmissionScores(t, f)
	foreign := f.flags(t, 1, 1, 2)
	own := f.flags(t, 0, 3, 1)
	values := []string{foreign[0], foreign[1], foreign[1], own[0], "unknown"}
	var first domain.SubmissionResult
	results, trace, err := submitWithStaleFlagRead(t, f, values, func() {
		winner, winnerErr := f.repo.SubmitBatch(t.Context(), f.request(0, foreign[:1]))
		if winnerErr != nil || len(winner) != 1 || winner[0].Err != nil {
			t.Fatalf("A must commit after B read its stale flags: %+v, %v", winner, winnerErr)
		}
		first = winner[0]
	})
	if err != nil || len(results) != len(values) {
		t.Fatalf("stale partial submission: %+v, %v", results, err)
	}
	for i, want := range []error{domain.ErrFlagAlreadyStolen, nil, domain.ErrFlagAlreadyStolen, domain.ErrOwnFlag, domain.ErrFlagNotFound} {
		if !errors.Is(results[i].Err, want) {
			t.Fatalf("verdict %d=%v, want %v", i, results[i].Err, want)
		}
	}
	afterA := 1200 + first.Attack.AttackerDelta
	victimAfterA := 3800 + first.Attack.VictimDelta
	change, err := domain.CalculateRatingChange(afterA, victimAfterA, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(results[1].Attack.AttackerDelta-change.Attacker) > 1e-10 || math.Abs(results[1].Attack.VictimDelta-change.Victim) > 1e-10 {
		t.Fatalf("fallback retained speculative score mutation: got %+v, want %+v", results[1].Attack, change)
	}
	assertSubmissionCell(t, f, 0, afterA+change.Attacker, 2, 0)
	assertSubmissionCell(t, f, 1, victimAfterA+change.Victim, 0, 2)
	assertSubmissionEventDeltas(t, f, map[string]float64{
		first.Attack.EventID:      first.Attack.AttackerDelta,
		results[1].Attack.EventID: change.Attacker,
	})
	if trace.fast.Load() != 1 || trace.fallback.Load() != 1 || trace.legacy.Load() != 0 || trace.commits.Load() != 1 {
		t.Fatalf("partial branch fast/fallback/legacy/commit=%d/%d/%d/%d", trace.fast.Load(), trace.fallback.Load(), trace.legacy.Load(), trace.commits.Load())
	}
}

func TestFastSubmissionStaleReadZeroConflictNeedsNoCommit(t *testing.T) {
	f := newSubmissionFixture(t)
	seedAsymmetricSubmissionScores(t, f)
	foreign := f.flags(t, 1, 1, 1)
	var before string
	var first domain.SubmissionResult
	results, trace, err := submitWithStaleFlagRead(t, f, []string{foreign[0], foreign[0], "unknown"}, func() {
		winner, winnerErr := f.repo.SubmitBatch(t.Context(), f.request(0, foreign))
		if winnerErr != nil || len(winner) != 1 || winner[0].Err != nil {
			t.Fatalf("A: %+v, %v", winner, winnerErr)
		}
		first = winner[0]
		before = submissionScoreSnapshot(t, f)
	})
	if err != nil || len(results) != 3 {
		t.Fatalf("stale zero insertion: %+v, %v", results, err)
	}
	for i, want := range []error{domain.ErrFlagAlreadyStolen, domain.ErrFlagAlreadyStolen, domain.ErrFlagNotFound} {
		if !errors.Is(results[i].Err, want) {
			t.Fatalf("zero verdict %d=%v, want %v", i, results[i].Err, want)
		}
	}
	if after := submissionScoreSnapshot(t, f); after != before {
		t.Fatalf("zero insertion changed scores: before=%s after=%s", before, after)
	}
	assertSubmissionEventDeltas(t, f, map[string]float64{
		first.Attack.EventID: first.Attack.AttackerDelta,
	})
	if trace.fast.Load() != 1 || trace.fallback.Load() != 0 || trace.legacy.Load() != 0 || trace.commits.Load() != 0 {
		t.Fatalf("zero branch fast/fallback/legacy/commit=%d/%d/%d/%d", trace.fast.Load(), trace.fallback.Load(), trace.legacy.Load(), trace.commits.Load())
	}
}

func TestFastSubmissionFullPathStorageFaultsRollBackEverything(t *testing.T) {
	for _, fault := range []struct {
		name, sql, drop string
	}{
		{
			name: "insert failure",
			sql: `CREATE FUNCTION fail_fast_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'event insert unavailable'; END $$;
CREATE TRIGGER fail_fast_insert BEFORE INSERT ON StolenFlags FOR EACH ROW EXECUTE FUNCTION fail_fast_insert()`,
			drop: `DROP TRIGGER fail_fast_insert ON StolenFlags`,
		},
		{
			name: "one score row skipped",
			sql: `CREATE FUNCTION skip_fast_score() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.stolen>OLD.stolen THEN RETURN NULL; END IF; RETURN NEW; END $$;
CREATE TRIGGER skip_fast_score BEFORE UPDATE ON TeamTasks FOR EACH ROW EXECUTE FUNCTION skip_fast_score()`,
			drop: `DROP TRIGGER skip_fast_score ON TeamTasks`,
		},
	} {
		t.Run(fault.name, func(t *testing.T) {
			f := newSubmissionFixture(t)
			seedAsymmetricSubmissionScores(t, f)
			values := f.flags(t, 1, 1, 2)
			before := submissionScoreSnapshot(t, f)
			if _, err := f.pool.Exec(t.Context(), fault.sql); err != nil {
				t.Fatal(err)
			}
			results, trace, err := submitWithStaleFlagRead(t, f, values, func() {
			})
			if err == nil || results != nil {
				t.Fatalf("full-path fault produced verdicts: %+v, %v", results, err)
			}
			if after := submissionScoreSnapshot(t, f); after != before {
				t.Fatalf("full-path fault changed scores: before=%s after=%s", before, after)
			}
			var records int
			if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM StolenFlags`).Scan(&records); err != nil {
				t.Fatal(err)
			}
			if records != 0 {
				t.Fatalf("full-path fault left %d accepted records", records)
			}
			assertSubmissionEventDeltas(t, f, map[string]float64{})
			if trace.fast.Load() != 1 || trace.fallback.Load() != 0 || trace.legacy.Load() != 0 || trace.commits.Load() != 0 {
				t.Fatalf("full-path fault must abort, not fallback: fast/fallback/legacy/commit=%d/%d/%d/%d", trace.fast.Load(), trace.fallback.Load(), trace.legacy.Load(), trace.commits.Load())
			}
			if _, err := f.pool.Exec(t.Context(), fault.drop); err != nil {
				t.Fatal(err)
			}
			retry, err := f.repo.SubmitBatch(t.Context(), f.request(0, values))
			if err != nil || len(retry) != 2 || retry[0].Err != nil || retry[1].Err != nil {
				t.Fatalf("failed fast path consumed events before retry: %+v, %v", retry, err)
			}
			assertSubmissionEventDeltas(t, f, map[string]float64{
				retry[0].Attack.EventID: retry[0].Attack.AttackerDelta,
				retry[1].Attack.EventID: retry[1].Attack.AttackerDelta,
			})
		})
	}
}

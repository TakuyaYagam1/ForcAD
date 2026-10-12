package postgres_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
)

func TestSubmissionDoesNotLockImmutableFlagRecords(t *testing.T) {
	f := newSubmissionFixture(t)
	values := f.flags(t, 1, 1, 1)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	// A metadata update must not serialize reads of the immutable flag value.
	if _, err := tx.Exec(t.Context(), `SELECT id FROM Flags WHERE flag=$1 FOR NO KEY UPDATE`, values[0]); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	results, err := f.repo.SubmitBatch(ctx, f.request(0, values))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("submission: %+v", results)
	}
}

type commitTrace struct{ commits atomic.Int64 }

func (trace *commitTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == "commit" {
		trace.commits.Add(1)
	}
	return ctx
}
func (*commitTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestRejectedOnlyBatchNeedsNoDurableCommit(t *testing.T) {
	f := newSubmissionFixture(t)
	foreign := f.flags(t, 1, 1, 1)
	own := f.flags(t, 0, 2, 1)
	trace := &commitTrace{}
	settings := f.pool.Config()
	settings.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := postgres.New(pool)
	if _, err := repo.SubmitBatch(t.Context(), f.request(0, foreign)); err != nil {
		t.Fatal(err)
	}
	if trace.commits.Swap(0) != 1 {
		t.Fatal("accepted flags require a durable commit")
	}
	for _, value := range []string{foreign[0], own[0], "unknown"} {
		results, err := repo.SubmitBatch(t.Context(), f.request(0, []string{value}))
		if err != nil || len(results) != 1 || results[0].Err == nil {
			t.Fatalf("rejected batch: %+v, %v", results, err)
		}
	}
	if got := trace.commits.Load(); got != 0 {
		t.Fatalf("verdict-only batches issued %d unnecessary commits", got)
	}
}

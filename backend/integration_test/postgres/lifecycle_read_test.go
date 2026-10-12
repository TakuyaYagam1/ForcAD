package postgres_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
)

type lifecycleTrace struct{ queries atomic.Int64 }

func (trace *lifecycleTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	trace.queries.Add(1)
	return ctx
}
func (*lifecycleTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestLifecycleReadsMetadataInOneQuery(t *testing.T) {
	f := newSubmissionFixture(t)
	trace := &lifecycleTrace{}
	settings := f.pool.Config()
	settings.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	game := postgres.NewGameStore(pool)
	life, err := game.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := trace.queries.Load(); got != 1 {
		t.Fatalf("lifecycle metadata required %d SQL commands, want one snapshot read", got)
	}
	if !life.Config.GameRunning || life.Session.Generation != f.generation || len(life.Session.Snapshot) != 0 {
		t.Fatalf("metadata read fetched stale state or the full scoreboard: %+v", life)
	}
	board, err := game.Board(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Lifecycle.Session.Snapshot) == 0 {
		t.Fatal("board read lost its completed-round snapshot")
	}
}

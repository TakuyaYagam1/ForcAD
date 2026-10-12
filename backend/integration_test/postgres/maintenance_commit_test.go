package postgres_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
)

func TestIdleSchedulerNeedsNoDurableCommit(t *testing.T) {
	f := newSubmissionFixture(t)
	trace := &commitTrace{}
	settings := f.pool.Config()
	settings.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := postgres.New(pool)
	started, err := repository.StartScheduled(t.Context(), time.Now())
	if err != nil || started {
		t.Fatalf("running game was restarted: %t, %v", started, err)
	}
	maintained, err := repository.Maintain(t.Context(), time.Now())
	if err != nil || maintained {
		t.Fatalf("running game was changed: %t, %v", maintained, err)
	}
	if got := trace.commits.Load(); got != 0 {
		t.Fatalf("idle scheduler issued %d unnecessary commits", got)
	}
}

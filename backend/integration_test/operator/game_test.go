package operator_test

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/bootstrap"
)

func TestPauseResumeCommandsPreserveGameAndRejectFinishedSession(t *testing.T) {
	pool, dsn := testutil.Database(t)
	t.Setenv("DATABASE_URL", dsn)
	document, err := config.LoadDocument(filepath.Join("..", "fixtures", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(t.Context(), document); err != nil {
		t.Fatal(err)
	}
	if err := store.ChangeState(t.Context(), "start_final", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.RunDatabaseCommand(t.Context(), "pause", nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	paused, err := store.Lifecycle(t.Context())
	if err != nil || paused.Session.PausedAt == nil || !paused.Config.GameRunning {
		t.Fatalf("pause: lifecycle=%+v error=%v", paused, err)
	}
	if err := bootstrap.RunDatabaseCommand(t.Context(), "pause", nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	repeated, err := store.Lifecycle(t.Context())
	if err != nil || repeated.Session.PausedAt == nil || !repeated.Session.PausedAt.Equal(*paused.Session.PausedAt) {
		t.Fatalf("repeated pause changed the clock: %v", err)
	}
	if err := bootstrap.RunDatabaseCommand(t.Context(), "resume", nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	resumed, err := store.Lifecycle(t.Context())
	if err != nil || resumed.Session.PausedAt != nil || !resumed.Config.GameRunning || resumed.Config.RealRound != paused.Config.RealRound || resumed.Session.Generation != paused.Session.Generation || resumed.Session.PausedSeconds <= 0 {
		t.Fatalf("resume: lifecycle=%+v error=%v", resumed, err)
	}
	if err := store.ChangeState(t.Context(), "finish", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.RunDatabaseCommand(t.Context(), "resume", nil, io.Discard); err == nil {
		t.Fatal("CLI resumed a finished game")
	}
}

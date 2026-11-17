package postgres_test

import (
	"errors"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func gameSettingsInput(life domain.Lifecycle) domain.GameSettingsUpdate {
	return domain.GameSettingsUpdate{
		Generation:       life.Session.Generation,
		Rounds:           life.Config.Rounds,
		FlagLifetime:     life.Config.FlagLifetime,
		GameHardness:     life.Config.GameHardness,
		Inflation:        life.Config.Inflation,
		VolgaAttacksMode: life.Config.VolgaAttacksMode,
		RoundTime:        life.Config.RoundTime,
		ExtraChecks:      life.Config.ExtraChecks,
		Mode:             life.Config.Mode,
		Timezone:         life.Config.Timezone,
		StartTime:        life.Config.StartTime,
	}
}

func TestUpdateGameSettingsReturnsUpdatedLifecycle(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(t.Context(), gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	input := gameSettingsInput(before)
	input.Rounds = new(int32(40))
	input.FlagLifetime = 5
	input.GameHardness = 12
	input.Inflation = false
	input.VolgaAttacksMode = true
	input.RoundTime = 90
	input.StartTime = input.StartTime.Add(time.Hour)

	after, err := store.UpdateGameSettings(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if after.Config.Rounds == nil || *after.Config.Rounds != 40 || after.Config.FlagLifetime != 5 ||
		after.Config.GameHardness != 12 || after.Config.Inflation || !after.Config.VolgaAttacksMode ||
		after.Config.RoundTime != 90 || !after.Config.StartTime.Equal(input.StartTime) {
		t.Fatalf("updated lifecycle = %+v", after)
	}
	if runtime := after.Runtime(time.Now()); runtime.RoundTime != 90 || runtime.Generation != input.Generation {
		t.Fatalf("updated runtime = %+v", runtime)
	}
}

func TestUpdateGameSettingsRejectsStaleGenerationWithoutChanges(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(t.Context(), gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	input := gameSettingsInput(before)
	input.Generation++
	input.FlagLifetime++

	_, err = store.UpdateGameSettings(t.Context(), input)
	var stateErr *domain.StateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("UpdateGameSettings() error = %v, want StateError", err)
	}
	after, err := store.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Config.FlagLifetime != before.Config.FlagLifetime || after.Session.Generation != before.Session.Generation {
		t.Fatalf("stale update changed lifecycle: before=%+v after=%+v", before, after)
	}
}

func TestUpdateGameSettingsRollsBackConstraintFailure(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(t.Context(), gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	input := gameSettingsInput(before)
	input.FlagLifetime = 99
	input.Mode = "blitz"
	input.ExtraChecks = 1

	if _, err = store.UpdateGameSettings(t.Context(), input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("UpdateGameSettings() error = %v, want ErrConflict", err)
	}
	after, err := store.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Config.FlagLifetime != before.Config.FlagLifetime || after.Config.Mode != before.Config.Mode ||
		after.Config.ExtraChecks != before.Config.ExtraChecks {
		t.Fatalf("failed update was partially committed: before=%+v after=%+v", before.Config, after.Config)
	}
}

func TestUpdateGameSettingsAllowsOnlyFutureRoundLimitWhileRunning(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(t.Context(), gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	if err := store.ChangeState(t.Context(), "start_final", new(int64(0)), time.Now()); err != nil {
		t.Fatal(err)
	}
	running, err := store.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	input := gameSettingsInput(running)
	input.Rounds = new(int32(10))
	if _, err = store.UpdateGameSettings(t.Context(), input); err != nil {
		t.Fatalf("round-only update failed: %v", err)
	}

	input.FlagLifetime++
	var stateErr *domain.StateError
	if _, err = store.UpdateGameSettings(t.Context(), input); !errors.As(err, &stateErr) {
		t.Fatalf("active non-round update error = %v, want StateError", err)
	}
}

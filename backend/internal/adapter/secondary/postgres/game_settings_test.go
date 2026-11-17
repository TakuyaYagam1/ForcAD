package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func gameSettingsStateFixture() (domain.Lifecycle, domain.GameSettingsUpdate, time.Time) {
	now := time.Date(2026, time.November, 17, 12, 0, 0, 0, time.UTC)
	life := domain.Lifecycle{
		Config: domain.GameConfig{
			FlagLifetime:     3,
			GameHardness:     10,
			Inflation:        true,
			VolgaAttacksMode: false,
			RoundTime:        60,
			ExtraChecks:      0,
			Rounds:           new(int32(20)),
			Mode:             "classic",
			Timezone:         "UTC",
			StartTime:        now.Add(24 * time.Hour),
		},
		Session: domain.Session{
			Generation: 7,
		},
	}
	input := domain.GameSettingsUpdate{
		Generation:       life.Session.Generation,
		Rounds:           new(*life.Config.Rounds),
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
	return life, input, now
}

func TestValidateGameSettingsStateRejectsStaleGeneration(t *testing.T) {
	life, input, now := gameSettingsStateFixture()
	input.Generation--

	err := validateGameSettingsState(life, input, now)
	var stateErr *domain.StateError
	if !errors.As(err, &stateErr) || stateErr.Message != "Game session has changed" {
		t.Fatalf("validateGameSettingsState() error = %v, want stale-generation StateError", err)
	}
}

func TestValidateGameSettingsStateAllowsOnlyRoundLimitDuringActiveGame(t *testing.T) {
	life, input, now := gameSettingsStateFixture()
	life.Config.GameRunning = true
	life.Config.RealRound = 5
	input.Rounds = new(int32(6))

	if err := validateGameSettingsState(life, input, now); err != nil {
		t.Fatalf("valid active round limit rejected: %v", err)
	}

	input.FlagLifetime++
	var stateErr *domain.StateError
	if err := validateGameSettingsState(life, input, now); !errors.As(err, &stateErr) {
		t.Fatalf("active non-round change error = %v, want StateError", err)
	}
}

func TestValidateGameSettingsStateRejectsReachedRoundLimit(t *testing.T) {
	life, input, now := gameSettingsStateFixture()
	life.Config.GameRunning = true
	life.Config.RealRound = 5
	input.Rounds = new(int32(5))

	var stateErr *domain.StateError
	if err := validateGameSettingsState(life, input, now); !errors.As(err, &stateErr) {
		t.Fatalf("reached round limit error = %v, want StateError", err)
	}
}

func TestValidateGameSettingsStateRejectsFinishedGame(t *testing.T) {
	life, input, now := gameSettingsStateFixture()
	life.Config.RealRound = 5
	input.Rounds = new(int32(30))

	var stateErr *domain.StateError
	if err := validateGameSettingsState(life, input, now); !errors.As(err, &stateErr) {
		t.Fatalf("finished game update error = %v, want StateError", err)
	}
}

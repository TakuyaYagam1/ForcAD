package usecase

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type gameSettingsRepository struct {
	outbound.GameRepository

	calls  int
	input  domain.GameSettingsUpdate
	result domain.Lifecycle
	err    error
}

func (r *gameSettingsRepository) UpdateGameSettings(_ context.Context, input domain.GameSettingsUpdate) (domain.Lifecycle, error) {
	r.calls++
	r.input = input
	return r.result, r.err
}

func validGameSettingsUpdate() domain.GameSettingsUpdate {
	return domain.GameSettingsUpdate{
		Generation:       4,
		Rounds:           new(int32(300)),
		FlagLifetime:     3,
		GameHardness:     10,
		Inflation:        true,
		VolgaAttacksMode: false,
		RoundTime:        60,
		ExtraChecks:      0,
		Mode:             "classic",
		Timezone:         "UTC",
		StartTime:        time.Date(2050, time.January, 2, 3, 4, 5, 0, time.UTC),
	}
}

func TestUpdateGameSettingsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.GameSettingsUpdate)
	}{
		{
			name: "nonpositive round limit",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.Rounds = new(int32(0))
			},
		},
		{
			name: "nonpositive flag lifetime",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.FlagLifetime = 0
			},
		},
		{
			name: "nonpositive round time",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.RoundTime = 0
			},
		},
		{
			name: "negative extra checks",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.ExtraChecks = -1
			},
		},
		{
			name: "too many extra checks",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.ExtraChecks = 9
			},
		},
		{
			name: "extra checks in blitz mode",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.Mode = "blitz"
				input.ExtraChecks = 1
			},
		},
		{
			name: "unknown mode",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.Mode = "other"
			},
		},
		{
			name: "empty timezone",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.Timezone = ""
			},
		},
		{
			name: "unknown timezone",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.Timezone = "Mars/Olympus_Mons"
			},
		},
		{
			name: "process local timezone",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.Timezone = "Local"
			},
		},
		{
			name: "zero start time",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.StartTime = time.Time{}
			},
		},
		{
			name: "hardness at lower bound",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.GameHardness = 1
			},
		},
		{
			name: "not a number hardness",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.GameHardness = math.NaN()
			},
		},
		{
			name: "infinite hardness",
			mutate: func(input *domain.GameSettingsUpdate) {
				input.GameHardness = math.Inf(1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &gameSettingsRepository{}
			game := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
			t.Cleanup(game.Close)
			input := validGameSettingsUpdate()
			test.mutate(&input)

			_, err := game.UpdateGameSettings(t.Context(), input)
			var validation *domain.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("UpdateGameSettings() error = %v, want ValidationError", err)
			}
			if repo.calls != 0 {
				t.Fatalf("repository calls = %d, want 0", repo.calls)
			}
		})
	}
}

func TestUpdateGameSettingsReturnsLifecycleAndSignalsChange(t *testing.T) {
	input := validGameSettingsUpdate()
	want := domain.Lifecycle{
		Config: domain.GameConfig{
			Rounds:       new(int32(400)),
			FlagLifetime: 5,
		},
		Session: domain.Session{
			Generation: input.Generation,
		},
	}
	repo := &gameSettingsRepository{
		result: want,
	}
	game := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(game.Close)

	got, err := game.UpdateGameSettings(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.input.Generation != input.Generation || repo.input.StartTime != input.StartTime {
		t.Fatalf("repository call = %d, %+v, want one call with %+v", repo.calls, repo.input, input)
	}
	if got.Session.Generation != want.Session.Generation || got.Config.FlagLifetime != want.Config.FlagLifetime ||
		got.Config.Rounds == nil || *got.Config.Rounds != *want.Config.Rounds {
		t.Fatalf("UpdateGameSettings() = %+v, want %+v", got, want)
	}
	if len(game.changed) != 1 {
		t.Fatalf("change signals = %d, want 1", len(game.changed))
	}
}

func TestUpdateGameSettingsDoesNotSignalFailedMutation(t *testing.T) {
	stateErr := &domain.StateError{
		Message: "Game session has changed",
	}
	repo := &gameSettingsRepository{
		err: stateErr,
	}
	game := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(game.Close)

	_, err := game.UpdateGameSettings(t.Context(), validGameSettingsUpdate())
	if !errors.Is(err, stateErr) {
		t.Fatalf("UpdateGameSettings() error = %v, want wrapped state error", err)
	}
	if len(game.changed) != 0 {
		t.Fatalf("change signals = %d, want 0", len(game.changed))
	}
}

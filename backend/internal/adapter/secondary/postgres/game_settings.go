package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (g *GameStore) UpdateGameSettings(ctx context.Context, input domain.GameSettingsUpdate) (domain.Lifecycle, error) {
	const location = "postgres.GameStore.UpdateGameSettings"
	var updated domain.Lifecycle
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		if _, err := q.LockSessionForUpdate(ctx); err != nil {
			return fmt.Errorf("%s - lock game session: %w", location, err)
		}
		life, err := readLifecycle(ctx, q, false)
		if err != nil {
			return fmt.Errorf("%s - read locked game lifecycle: %w", location, err)
		}
		if err := validateGameSettingsState(life, input, time.Now()); err != nil {
			return fmt.Errorf("%s - validate settings state: %w", location, err)
		}
		rows, err := q.UpdateGameSettings(ctx, sqlc.UpdateGameSettingsParams{
			FlagLifetime:     input.FlagLifetime,
			GameHardness:     input.GameHardness,
			Inflation:        input.Inflation,
			VolgaAttacksMode: input.VolgaAttacksMode,
			RoundTime:        input.RoundTime,
			Rounds:           input.Rounds,
			ExtraChecks:      input.ExtraChecks,
			Mode:             input.Mode,
			Timezone:         input.Timezone,
			StartTime:        gameStamp(input.StartTime),
		})
		if err != nil {
			return fmt.Errorf("%s - update game configuration: %w", location, err)
		}
		if rows != 1 {
			return fmt.Errorf("%s - update game configuration: %w", location, domain.ErrNotFound)
		}
		updated, err = readLifecycle(ctx, q, false)
		if err != nil {
			return fmt.Errorf("%s - read updated game lifecycle: %w", location, err)
		}
		return nil
	})
	if err != nil {
		return domain.Lifecycle{}, fmt.Errorf("%s - commit settings update: %w", location, err)
	}
	return updated, nil
}

func validateGameSettingsState(life domain.Lifecycle, input domain.GameSettingsUpdate, now time.Time) error {
	const location = "postgres.validateGameSettingsState"
	if input.Generation != life.Session.Generation {
		return fmt.Errorf("%s - validate session generation: %w", location, &domain.StateError{
			Message: "Game session has changed",
		})
	}
	if life.Finished() {
		return fmt.Errorf("%s - validate terminal lifecycle: %w", location, &domain.StateError{
			Message: "Game settings cannot be changed after the game has finished",
		})
	}
	if nonRoundSettingsChanged(life.Config, input) && !life.Runtime(now).CanStart {
		return fmt.Errorf("%s - validate active lifecycle: %w", location, &domain.StateError{
			Message: "Only the round limit can be changed after the game has started",
		})
	}
	if life.Config.GameRunning && input.Rounds != nil && *input.Rounds <= life.Config.RealRound {
		return fmt.Errorf("%s - validate active round limit: %w", location, &domain.StateError{
			Message: "Round limit must be greater than the current round",
		})
	}
	return nil
}

func nonRoundSettingsChanged(current domain.GameConfig, input domain.GameSettingsUpdate) bool {
	return current.FlagLifetime != input.FlagLifetime ||
		current.GameHardness != input.GameHardness ||
		current.Inflation != input.Inflation ||
		current.VolgaAttacksMode != input.VolgaAttacksMode ||
		current.RoundTime != input.RoundTime ||
		current.ExtraChecks != input.ExtraChecks ||
		current.Mode != input.Mode ||
		current.Timezone != input.Timezone ||
		!current.StartTime.Equal(input.StartTime)
}

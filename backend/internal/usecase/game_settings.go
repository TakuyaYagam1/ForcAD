package usecase

import (
	"context"
	"fmt"
	"math"
	"time"
	_ "time/tzdata" // Keep IANA timezones available in the minimal runtime image.

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

const maxExtraChecks = 8

func (g *Game) UpdateGameSettings(ctx context.Context, input domain.GameSettingsUpdate) (domain.Lifecycle, error) {
	const location = "usecase.Game.UpdateGameSettings"
	if err := validateGameSettings(input); err != nil {
		return domain.Lifecycle{}, fmt.Errorf("%s - validate game settings: %w", location, err)
	}
	life, err := g.repo.UpdateGameSettings(ctx, input)
	if err != nil {
		return domain.Lifecycle{}, fmt.Errorf("%s - update game settings: %w", location, err)
	}
	g.Changed()
	return life, nil
}

func validateGameSettings(input domain.GameSettingsUpdate) error {
	const location = "usecase.validateGameSettings"
	if input.Rounds != nil && *input.Rounds < 1 {
		return fmt.Errorf("%s - validate round limit: %w", location, &domain.ValidationError{
			Message: "Round limit must be positive or null",
		})
	}
	if input.FlagLifetime < 1 {
		return fmt.Errorf("%s - validate flag lifetime: %w", location, &domain.ValidationError{
			Message: "Flag lifetime must be positive",
		})
	}
	if input.RoundTime < 1 {
		return fmt.Errorf("%s - validate round duration: %w", location, &domain.ValidationError{
			Message: "Round duration must be positive",
		})
	}
	if input.ExtraChecks < 0 || input.ExtraChecks > maxExtraChecks {
		return fmt.Errorf("%s - validate extra check count: %w", location, &domain.ValidationError{
			Message: "Extra check count must be between 0 and 8",
		})
	}
	if input.Mode != "classic" && input.Mode != "blitz" {
		return fmt.Errorf("%s - validate game mode: %w", location, &domain.ValidationError{
			Message: "Game mode must be classic or blitz",
		})
	}
	if input.ExtraChecks > 0 && input.Mode != "classic" {
		return fmt.Errorf("%s - validate extra check mode: %w", location, &domain.ValidationError{
			Message: "Extra checks require classic mode",
		})
	}
	if err := validateGameSettingsTime(input); err != nil {
		return err
	}
	if math.IsNaN(input.GameHardness) || math.IsInf(input.GameHardness, 0) || input.GameHardness <= 1 {
		return fmt.Errorf("%s - validate game hardness: %w", location, &domain.ValidationError{
			Message: "Game hardness must be finite and greater than 1",
		})
	}
	return nil
}

func validateGameSettingsTime(input domain.GameSettingsUpdate) error {
	const location = "usecase.validateGameSettingsTime"
	if input.Timezone == "" {
		return fmt.Errorf("%s - validate game timezone: %w", location, &domain.ValidationError{
			Message: "Game timezone is required",
		})
	}
	if input.Timezone == "Local" {
		return fmt.Errorf("%s - validate game timezone: %w", location, &domain.ValidationError{
			Message: "Game timezone must be a valid IANA timezone",
		})
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return fmt.Errorf("%s - validate game timezone: %w", location, &domain.ValidationError{
			Message: "Game timezone must be a valid IANA timezone",
		})
	}
	if input.StartTime.IsZero() {
		return fmt.Errorf("%s - validate scheduled start: %w", location, &domain.ValidationError{
			Message: "Scheduled start is required",
		})
	}
	return nil
}

package usecase

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

const maxBulkIDs = 100

func (g *Game) ActivateTeams(ctx context.Context, teamIDs []uuid.UUID) (int32, error) {
	const location = "usecase.Game.ActivateTeams"
	if err := validateBulkIDs(teamIDs); err != nil {
		return 0, fmt.Errorf("%s - validate team IDs: %w", location, err)
	}
	count, err := g.repo.ActivateTeams(ctx, teamIDs)
	if err != nil {
		return 0, fmt.Errorf("%s - activate teams: %w", location, err)
	}
	if count > 0 {
		g.Changed()
	}
	return count, nil
}

func (g *Game) ActivateTasks(ctx context.Context, taskIDs []uuid.UUID) (int32, error) {
	const location = "usecase.Game.ActivateTasks"
	if err := validateBulkIDs(taskIDs); err != nil {
		return 0, fmt.Errorf("%s - validate task IDs: %w", location, err)
	}
	count, err := g.repo.ActivateTasks(ctx, taskIDs)
	if err != nil {
		return 0, fmt.Errorf("%s - activate tasks: %w", location, err)
	}
	if count > 0 {
		g.Changed()
	}
	return count, nil
}

func (g *Game) DeactivateTeams(ctx context.Context, teamIDs []uuid.UUID) (int32, error) {
	const location = "usecase.Game.DeactivateTeams"
	if err := validateBulkIDs(teamIDs); err != nil {
		return 0, fmt.Errorf("%s - validate team IDs: %w", location, err)
	}
	count, err := g.repo.DeactivateTeams(ctx, teamIDs)
	if err != nil {
		return 0, fmt.Errorf("%s - deactivate teams: %w", location, err)
	}
	if count > 0 {
		g.Changed()
	}
	return count, nil
}

func (g *Game) DeactivateTasks(ctx context.Context, taskIDs []uuid.UUID) (int32, error) {
	const location = "usecase.Game.DeactivateTasks"
	if err := validateBulkIDs(taskIDs); err != nil {
		return 0, fmt.Errorf("%s - validate task IDs: %w", location, err)
	}
	count, err := g.repo.DeactivateTasks(ctx, taskIDs)
	if err != nil {
		return 0, fmt.Errorf("%s - deactivate tasks: %w", location, err)
	}
	if count > 0 {
		g.Changed()
	}
	return count, nil
}

func validateBulkIDs(ids []uuid.UUID) error {
	if len(ids) == 0 || len(ids) > maxBulkIDs {
		return &domain.ValidationError{
			Message: "ID count must be between 1 and 100",
		}
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil() {
			return &domain.ValidationError{
				Message: "IDs must not contain a nil UUID",
			}
		}
		if _, exists := seen[id]; exists {
			return &domain.ValidationError{
				Message: "IDs must be unique",
			}
		}
		seen[id] = struct{}{}
	}
	return nil
}

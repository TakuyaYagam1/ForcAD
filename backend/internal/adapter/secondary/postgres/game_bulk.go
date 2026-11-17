package postgres

import (
	"context"
	"fmt"
	"math"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (g *GameStore) ActivateTeams(ctx context.Context, teamIDs []uuid.UUID) (int32, error) {
	const location = "postgres.GameStore.ActivateTeams"
	var activated int32
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		existing, err := q.CountExistingTeams(ctx, teamIDs)
		if err != nil {
			return fmt.Errorf("%s - count existing teams: %w", location, err)
		}
		if int(existing) != len(teamIDs) {
			return fmt.Errorf("%s - validate requested teams: %w", location, domain.ErrNotFound)
		}
		affected, err := q.ActivateTeams(ctx, teamIDs)
		if err != nil {
			return fmt.Errorf("%s - activate teams: %w", location, err)
		}
		if affected < 0 || affected > math.MaxInt32 {
			return fmt.Errorf("%s - invalid team activation count: %d", location, affected)
		}
		activated = int32(affected)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%s - commit team activation: %w", location, err)
	}
	return activated, nil
}

func (g *GameStore) ActivateTasks(ctx context.Context, taskIDs []uuid.UUID) (int32, error) {
	const location = "postgres.GameStore.ActivateTasks"
	var activated int32
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		existing, err := q.CountExistingTasks(ctx, taskIDs)
		if err != nil {
			return fmt.Errorf("%s - count existing tasks: %w", location, err)
		}
		if int(existing) != len(taskIDs) {
			return fmt.Errorf("%s - validate requested tasks: %w", location, domain.ErrNotFound)
		}
		affected, err := q.ActivateTasks(ctx, taskIDs)
		if err != nil {
			return fmt.Errorf("%s - activate tasks: %w", location, err)
		}
		if affected < 0 || affected > math.MaxInt32 {
			return fmt.Errorf("%s - invalid task activation count: %d", location, affected)
		}
		activated = int32(affected)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%s - commit task activation: %w", location, err)
	}
	return activated, nil
}

func (g *GameStore) DeactivateTeams(ctx context.Context, teamIDs []uuid.UUID) (int32, error) {
	const location = "postgres.GameStore.DeactivateTeams"
	var deactivated int32
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		existing, err := q.CountExistingTeams(ctx, teamIDs)
		if err != nil {
			return fmt.Errorf("%s - count existing teams: %w", location, err)
		}
		if int(existing) != len(teamIDs) {
			return fmt.Errorf("%s - validate requested teams: %w", location, domain.ErrNotFound)
		}
		affected, err := q.DeactivateTeams(ctx, teamIDs)
		if err != nil {
			return fmt.Errorf("%s - deactivate teams: %w", location, err)
		}
		if affected < 0 || affected > math.MaxInt32 {
			return fmt.Errorf("%s - invalid team deactivation count: %d", location, affected)
		}
		deactivated = int32(affected)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%s - commit team deactivation: %w", location, err)
	}
	return deactivated, nil
}

func (g *GameStore) DeactivateTasks(ctx context.Context, taskIDs []uuid.UUID) (int32, error) {
	const location = "postgres.GameStore.DeactivateTasks"
	var deactivated int32
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		existing, err := q.CountExistingTasks(ctx, taskIDs)
		if err != nil {
			return fmt.Errorf("%s - count existing tasks: %w", location, err)
		}
		if int(existing) != len(taskIDs) {
			return fmt.Errorf("%s - validate requested tasks: %w", location, domain.ErrNotFound)
		}
		affected, err := q.DeactivateTasks(ctx, taskIDs)
		if err != nil {
			return fmt.Errorf("%s - deactivate tasks: %w", location, err)
		}
		if affected < 0 || affected > math.MaxInt32 {
			return fmt.Errorf("%s - invalid task deactivation count: %d", location, affected)
		}
		deactivated = int32(affected)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%s - commit task deactivation: %w", location, err)
	}
	return deactivated, nil
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) ActiveIDByToken(ctx context.Context, token string) (uuid.UUID, error) {
	const location = "postgres.Repository.ActiveIDByToken"

	id, err := r.queries.ActiveTeamIDByToken(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil(), fmt.Errorf("%s - find active team by token: %w", location, domain.ErrTeamTokenInvalid)
	}
	if err != nil {
		return uuid.Nil(), fmt.Errorf("%s - find active team by token: %w", location, err)
	}
	return id, nil
}

func (r *Repository) IsActiveTeam(ctx context.Context, teamID uuid.UUID) (bool, error) {
	const location = "postgres.Repository.IsActiveTeam"

	active, err := r.queries.IsActiveTeamByID(ctx, teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("%s - check active team: %w", location, domain.ErrTeamTokenInvalid)
	}
	if err != nil {
		return false, fmt.Errorf("%s - check active team: %w", location, err)
	}
	return active, nil
}

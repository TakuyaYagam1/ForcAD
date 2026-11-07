package outbound

import (
	"context"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type TeamRepository interface {
	ActiveIDByToken(ctx context.Context, token string) (uuid.UUID, error)
	IsActiveTeam(ctx context.Context, teamID uuid.UUID) (bool, error)
}

type TeamCatalogRepository interface {
	Teams(ctx context.Context, active bool) ([]domain.Team, error)
	CreateTeam(ctx context.Context, team domain.Team) (domain.Team, error)
	UpdateTeam(ctx context.Context, team domain.Team) (domain.Team, error)
	DeactivateTeam(ctx context.Context, teamID uuid.UUID) error
}

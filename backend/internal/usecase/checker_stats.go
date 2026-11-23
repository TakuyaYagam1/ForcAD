package usecase

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (g *Game) CheckerStats(ctx context.Context, teamID uuid.UUID, round int32) ([]domain.TaskCheckerStats, error) {
	const location = "usecase.Game.CheckerStats"
	stats, err := g.repo.CheckerStats(ctx, teamID, round)
	if err != nil {
		return nil, fmt.Errorf("%s - load checker statistics: %w", location, err)
	}
	return stats, nil
}

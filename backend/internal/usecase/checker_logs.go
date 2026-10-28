package usecase

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

const (
	defaultCheckerLogLimit int32 = 100
	maxCheckerLogLimit     int32 = 500
)

func (g *Game) CheckerLogs(
	ctx context.Context,
	teamID, taskID uuid.UUID,
	beforeID *uuid.UUID,
	limit int32,
) (domain.CheckerLogPage, error) {
	const location = "usecase.Game.CheckerLogs"
	if limit <= 0 {
		limit = defaultCheckerLogLimit
	} else if limit > maxCheckerLogLimit {
		limit = maxCheckerLogLimit
	}
	rows, err := g.repo.CheckerLogs(ctx, teamID, taskID, beforeID, limit+1)
	if err != nil {
		return domain.CheckerLogPage{}, fmt.Errorf("%s - load checker logs: %w", location, err)
	}
	if rows == nil {
		rows = []domain.CheckerLog{}
	}
	page := domain.CheckerLogPage{
		TeamID: teamID, TaskID: taskID, Limit: limit,
		Logs: rows,
	}
	if len(rows) > int(limit) {
		page.HasMore = true
		page.Logs = rows[:limit]
		page.NextBeforeID = new(page.Logs[len(page.Logs)-1].ID)
	}
	return page, nil
}

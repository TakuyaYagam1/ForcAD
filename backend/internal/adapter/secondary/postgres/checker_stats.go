package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (g *GameStore) CheckerStats(ctx context.Context, teamID uuid.UUID, round int32) ([]domain.TaskCheckerStats, error) {
	const location = "postgres.GameStore.CheckerStats"
	rows, err := g.queries.ReadCheckerStats(ctx, sqlc.ReadCheckerStatsParams{
		TeamID: teamID,
		Round:  round,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - read scheduled actions and outcomes: %w", location, err)
	}
	// The query returns CHECK, GET and PUT contiguously for each active task.
	tasks := make([]domain.TaskCheckerStats, 0, len(rows)/3)
	for _, row := range rows {
		if len(tasks) == 0 || row.TaskID != tasks[len(tasks)-1].TaskID {
			tasks = append(tasks, domain.TaskCheckerStats{
				TaskID: row.TaskID,
				Name:   row.Name,
			})
		}
		task := &tasks[len(tasks)-1]
		count := domain.CheckerActionCount{
			Maximum:  row.Maximum,
			Reported: row.Reported,
			Failed:   row.Failed,
			NoResult: row.NoResult,
		}
		switch row.Action {
		case "check":
			task.Check = count
		case "put":
			task.Put = count
		case "get":
			task.Get = count
		}
	}
	return tasks, nil
}

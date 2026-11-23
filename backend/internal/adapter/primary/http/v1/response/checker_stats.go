package response

import (
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func CheckerStats(teamID uuid.UUID, round int32, rows []domain.TaskCheckerStats) *api.AdminCheckerStats {
	tasks := make([]api.AdminTaskCheckerStats, len(rows))
	for i, row := range rows {
		tasks[i] = api.AdminTaskCheckerStats{
			TaskID: api.UUID(row.TaskID),
			Name:   row.Name,
			Check:  checkerActionCount(row.Check),
			Put:    checkerActionCount(row.Put),
			Get:    checkerActionCount(row.Get),
		}
	}
	return &api.AdminCheckerStats{
		TeamID: api.UUID(teamID),
		Round:  round,
		Tasks:  tasks,
	}
}

func checkerActionCount(count domain.CheckerActionCount) api.AdminCheckerActionCount {
	return api.AdminCheckerActionCount{
		Maximum:  count.Maximum,
		Reported: count.Reported,
		Failed:   count.Failed,
		NoResult: count.NoResult,
	}
}

package response

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func PublicCheckerLog(log domain.CheckerLog) api.CheckerLog {
	log = log.PublicView()
	return api.CheckerLog{
		ID:            api.UUID(log.ID),
		Round:         log.Round,
		Action:        log.Action,
		Stage:         log.Stage,
		StartedAt:     log.StartedAt,
		FinishedAt:    log.FinishedAt,
		DurationMs:    log.DurationMS,
		TimedOut:      log.TimedOut,
		Status:        log.Status,
		PublicMessage: log.PublicMessage,
	}
}

func CheckerLogPage(page domain.CheckerLogPage) *api.CheckerLogPage {
	result := &api.CheckerLogPage{
		TeamID:  api.UUID(page.TeamID),
		TaskID:  api.UUID(page.TaskID),
		Limit:   page.Limit,
		HasMore: page.HasMore,
		Logs:    make([]api.CheckerLog, 0, len(page.Logs)),
	}
	if page.NextBeforeID != nil {
		result.NextBeforeID = api.NewOptUUID(api.UUID(*page.NextBeforeID))
	}
	for _, row := range page.Logs {
		result.Logs = append(result.Logs, PublicCheckerLog(row))
	}
	return result
}

package v1

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (h *teamTasksHandler) GetClientCheckerLogs(
	ctx context.Context,
	params api.GetClientCheckerLogsParams,
) (api.GetClientCheckerLogsRes, error) {
	const location = "httpv1.teamTasksHandler.GetClientCheckerLogs"
	limit := int32(100)
	if requested, ok := params.Limit.Get(); ok {
		if requested < 1 || requested > 500 {
			return &api.GetClientCheckerLogsBadRequest{Error: "limit must be between 1 and 500"}, nil
		}
		limit = requested
	}
	var beforeID *uuid.UUID
	if value, ok := params.BeforeID.Get(); ok {
		parsed, valid := parseCheckerLogCursor(value)
		if !valid {
			return &api.GetClientCheckerLogsBadRequest{Error: "before_id must be a valid UUID"}, nil
		}
		beforeID = &parsed
	}
	page, err := h.game.CheckerLogs(ctx, params.TeamID, params.TaskID, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("%s - load public checker logs: %w", location, err)
	}
	result := api.CheckerLogPage{
		TeamID: api.UUID(page.TeamID), TaskID: api.UUID(page.TaskID),
		Limit: page.Limit, HasMore: page.HasMore,
		Logs: make([]api.CheckerLog, 0, len(page.Logs)),
	}
	if page.NextBeforeID != nil {
		result.NextBeforeID = api.NewOptUUID(api.UUID(*page.NextBeforeID))
	}
	for _, row := range page.Logs {
		result.Logs = append(result.Logs, publicCheckerLog(row))
	}
	return &result, nil
}

func parseCheckerLogCursor(value string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(value)
	return parsed, err == nil
}

func publicCheckerLog(log domain.CheckerLog) api.CheckerLog {
	return api.CheckerLog{
		ID: api.UUID(log.ID), Round: log.Round, Action: log.Action, Stage: log.Stage,
		StartedAt: log.StartedAt, FinishedAt: log.FinishedAt, DurationMs: log.DurationMS,
		TimedOut: log.TimedOut, Status: log.Status, PublicMessage: log.PublicMessage,
	}
}

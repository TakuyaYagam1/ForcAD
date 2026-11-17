package v1

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/request"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/response"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

func (h *teamTasksHandler) GetClientCheckerLogs(ctx context.Context, params api.GetClientCheckerLogsParams) (api.GetClientCheckerLogsRes, error) {
	const location = "httpv1.teamTasksHandler.GetClientCheckerLogs"
	limit := int32(100)
	if requested, ok := params.Limit.Get(); ok {
		if requested < 1 || requested > 500 {
			return &api.GetClientCheckerLogsBadRequest{
				Error: "limit must be between 1 and 500",
			}, nil
		}
		limit = requested
	}
	var beforeID *uuid.UUID
	if value, ok := params.BeforeID.Get(); ok {
		parsed, valid := request.CheckerLogCursor(value)
		if !valid {
			return &api.GetClientCheckerLogsBadRequest{
				Error: "before_id must be a valid UUID",
			}, nil
		}
		beforeID = &parsed
	}
	page, err := h.game.CheckerLogs(ctx, params.TeamID, params.TaskID, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("%s - load public checker logs: %w", location, err)
	}
	return response.CheckerLogPage(page), nil
}

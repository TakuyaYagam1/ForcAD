package v1

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/response"
)

func (h *teamTasksHandler) GetAdminCheckerStats(ctx context.Context, params api.GetAdminCheckerStatsParams) (api.GetAdminCheckerStatsRes, error) {
	const location = "httpv1.teamTasksHandler.GetAdminCheckerStats"
	rows, err := h.game.CheckerStats(ctx, uuid.UUID(params.TeamID), params.Round)
	if err != nil {
		return nil, fmt.Errorf("%s - load team checker statistics: %w", location, err)
	}
	return response.CheckerStats(uuid.UUID(params.TeamID), params.Round, rows), nil
}

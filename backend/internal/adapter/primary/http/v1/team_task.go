package v1

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/response"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type teamTasksHandler struct {
	game GameService
}

func (h *teamTasksHandler) GetAdminTeamTaskHistory(ctx context.Context, params api.GetAdminTeamTaskHistoryParams) (api.GetAdminTeamTaskHistoryRes, error) {
	const location = "httpv1.teamTasksHandler.GetAdminTeamTaskHistory"
	rows, err := h.game.History(ctx, uuid.UUID(params.TeamID), new(uuid.UUID(params.TaskID)))
	if err != nil {
		return nil, fmt.Errorf("%s - load team task history: %w", location, err)
	}
	return response.AdminTeamTaskHistory(rows), nil
}

func (h *teamTasksHandler) GetClientTeamHistory(ctx context.Context, params api.GetClientTeamHistoryParams) ([]api.ParticipantTeamTaskHistory, error) {
	const location = "httpv1.teamTasksHandler.GetClientTeamHistory"
	rows, err := h.game.PublicHistory(ctx, params.TeamID)
	if err != nil {
		return nil, fmt.Errorf("%s - load public team history: %w", location, err)
	}
	return response.PublicTeamHistory(rows), nil
}

func (*teamTasksHandler) GetAdminTeamTask(context.Context, api.GetAdminTeamTaskParams) (api.GetAdminTeamTaskRes, error) {
	const location = "httpv1.teamTasksHandler.GetAdminTeamTask"
	return nil, fmt.Errorf("%s - team task route is not handled here: %w", location, domain.ErrNotFound)
}

func (*teamTasksHandler) CreateAdminTeamTask(context.Context) (api.CreateAdminTeamTaskRes, error) {
	const location = "httpv1.teamTasksHandler.CreateAdminTeamTask"
	return nil, fmt.Errorf("%s - team task route is not handled here: %w", location, domain.ErrNotFound)
}

func (*teamTasksHandler) UpdateAdminTeamTask(context.Context, api.UpdateAdminTeamTaskParams) (api.UpdateAdminTeamTaskRes, error) {
	const location = "httpv1.teamTasksHandler.UpdateAdminTeamTask"
	return nil, fmt.Errorf("%s - team task route is not handled here: %w", location, domain.ErrNotFound)
}

func (*teamTasksHandler) DeleteAdminTeamTask(context.Context, api.DeleteAdminTeamTaskParams) (api.DeleteAdminTeamTaskRes, error) {
	const location = "httpv1.teamTasksHandler.DeleteAdminTeamTask"
	return nil, fmt.Errorf("%s - team task route is not handled here: %w", location, domain.ErrNotFound)
}

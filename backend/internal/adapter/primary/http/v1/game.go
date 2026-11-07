package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/response"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/request"
)

type gameHandler struct {
	game GameService
}

func (h *gameHandler) ChangeAdminGameState(ctx context.Context, req api.OptGameActionRequest, params api.ChangeAdminGameStateParams) (api.ChangeAdminGameStateRes, error) {
	const location = "httpv1.gameHandler.ChangeAdminGameState"
	action := string(params.Action)
	generation, err := request.GameAction(req, action)
	if err != nil {
		return nil, fmt.Errorf("%s - validate game action: %w", location, err)
	}
	if err := h.game.ChangeState(ctx, action, generation); err != nil {
		return nil, fmt.Errorf("%s - change game state: %w", location, err)
	}
	runtime, err := h.game.Runtime(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read updated game status: %w", location, err)
	}
	return response.RuntimeStatus(runtime), nil
}

func (h *gameHandler) GetClientConfig(ctx context.Context) (*api.GameConfig, error) {
	const location = "httpv1.gameHandler.GetClientConfig"
	life, err := h.game.Lifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read game configuration: %w", location, err)
	}
	return new(response.GameConfig(life.Config)), nil
}

func (h *gameHandler) GetClientStatus(ctx context.Context) (*api.GameRuntimeStatus, error) {
	const location = "httpv1.gameHandler.GetClientStatus"
	runtime, err := h.game.Runtime(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read game status: %w", location, err)
	}
	return response.RuntimeStatus(runtime), nil
}

func (h *gameHandler) GetClientAttackData(ctx context.Context, params api.GetClientAttackDataParams, w http.ResponseWriter) error {
	const location = "httpv1.gameHandler.GetClientAttackData"
	service, hasService := params.Service.Get()
	var filter *string
	if hasService {
		filter = &service
	}
	ifNoneMatch, _ := params.IfNoneMatch.Get()
	if err := writeAttackData(ctx, h.game, filter, ifNoneMatch, w); err != nil {
		return fmt.Errorf("%s - write attack data: %w", location, err)
	}
	return nil
}

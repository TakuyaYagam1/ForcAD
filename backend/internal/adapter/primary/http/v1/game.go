package v1

import (
	"context"
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type gameHandler struct {
	game GameService
}

func (h *gameHandler) ChangeAdminGameState(ctx context.Context, req api.OptGameActionRequest, params api.ChangeAdminGameStateParams) (api.ChangeAdminGameStateRes, error) {
	const location = "httpv1.gameHandler.ChangeAdminGameState"
	action := string(params.Action)
	var generation *int64
	data, _ := req.Get()
	if (action == "finish" || action == "start_final") && !data.Confirm.Or(false) {
		return nil, fmt.Errorf("%s - game action confirmation is required: %w", location, &domain.ValidationError{Message: "Game action confirmation is required"})
	}
	if value, ok := data.Generation.Get(); ok {
		generation = &value
	}
	if err := h.game.ChangeState(ctx, action, generation); err != nil {
		return nil, fmt.Errorf("%s - change game state: %w", location, err)
	}
	runtime, err := h.game.Runtime(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read updated game status: %w", location, err)
	}
	return runtimeStatus(runtime), nil
}

func (h *gameHandler) GetClientConfig(ctx context.Context) (*api.GameConfig, error) {
	const location = "httpv1.gameHandler.GetClientConfig"
	life, err := h.game.Lifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read game configuration: %w", location, err)
	}
	return new(gameConfig(life.Config)), nil
}

func (h *gameHandler) GetClientStatus(ctx context.Context) (*api.GameRuntimeStatus, error) {
	const location = "httpv1.gameHandler.GetClientStatus"
	runtime, err := h.game.Runtime(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read game status: %w", location, err)
	}
	return runtimeStatus(runtime), nil
}

func (h *gameHandler) GetClientAttackData(ctx context.Context) (api.NilAttackData, error) {
	const location = "httpv1.gameHandler.GetClientAttackData"
	data, err := h.game.AttackData(ctx)
	if err != nil {
		return api.NilAttackData{}, fmt.Errorf("%s - load public attack data: %w", location, err)
	}
	result := make(api.AttackData, len(data))
	for name, teams := range data {
		result[name] = api.AttackDataItem(teams)
	}
	return api.NewNilAttackData(result), nil
}

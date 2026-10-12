package v1

import (
	"context"
	"fmt"
	"math"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

type scoreboardHandler struct {
	game GameService
}

func (h *scoreboardHandler) GetClientCtftimeScoreboard(ctx context.Context) (api.GetClientCtftimeScoreboardRes, error) {
	const location = "httpv1.scoreboardHandler.GetClientCtftimeScoreboard"
	rows, err := h.game.Standings(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - load standings: %w", location, err)
	}
	result := make(api.GetClientCtftimeScoreboardOKApplicationJSON, 0, len(rows))
	for _, row := range rows {
		if row.Pos < 1 || row.Pos > math.MaxInt32 {
			return nil, fmt.Errorf("%s - standings position exceeds API range", location)
		}
		result = append(result, api.CtftimeStanding{Pos: int32(row.Pos), Team: row.Team, Score: row.Score})
	}
	return &result, nil
}

func (h *scoreboardHandler) GetScoreboard(ctx context.Context) (*api.Scoreboard, error) {
	const location = "httpv1.scoreboardHandler.GetScoreboard"
	board, err := h.game.Scoreboard(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - load scoreboard: %w", location, err)
	}
	return scoreboard(board), nil
}

package v1

import (
	"context"
	"fmt"
	"math"
	"net/http"

	"github.com/go-faster/jx"

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

func (h *scoreboardHandler) GetScoreboard(ctx context.Context, w http.ResponseWriter) error {
	const location = "httpv1.scoreboardHandler.GetScoreboard"
	board, err := h.game.Scoreboard(ctx)
	if err != nil {
		return fmt.Errorf("%s - load scoreboard: %w", location, err)
	}
	e := jx.GetEncoder()
	defer func() {
		// Keep ordinary responses reusable without retaining unusually large boards.
		if cap(e.Bytes()) > 1<<20 {
			e.SetBytes(nil)
		}
		jx.PutEncoder(e)
	}()
	scoreboard(board).Encode(e)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := e.WriteTo(w); err != nil {
		return fmt.Errorf("%s - write scoreboard: %w", location, err)
	}
	return nil
}

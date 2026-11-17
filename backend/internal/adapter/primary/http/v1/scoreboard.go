package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/response"

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
	result, err := response.CtftimeStandings(rows)
	if err != nil {
		return nil, fmt.Errorf("%s - encode standings: %w", location, err)
	}
	return result, nil
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
	response.Scoreboard(board).Encode(e)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := e.WriteTo(w); err != nil {
		return fmt.Errorf("%s - write scoreboard: %w", location, err)
	}
	return nil
}

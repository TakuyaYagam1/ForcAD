package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type checkerStatsGame struct {
	GameService

	teamID uuid.UUID
	round  int32
}

func (g *checkerStatsGame) CheckerStats(_ context.Context, teamID uuid.UUID, round int32) ([]domain.TaskCheckerStats, error) {
	g.teamID, g.round = teamID, round
	return []domain.TaskCheckerStats{{
		TaskID: httpTaskID,
		Name:   "example",
		Put: domain.CheckerActionCount{
			Maximum:  5,
			Reported: 2,
			Failed:   1,
		},
	}}, nil
}

func TestCheckerStatsRequiresSessionAndScopedFilters(t *testing.T) {
	game := &checkerStatsGame{}
	router := testRouter(t, "api", game, testAuth{}, &testFlags{})
	path := "/api/v1/admin/checker-stats/?team_id=" + httpTeamID.String() + "&round="
	if res := performRequest(router, http.MethodGet, path+"4", "", nil); res.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated request returned %d", res.Code)
	}
	headers := map[string]string{
		"Cookie": "session=valid",
	}
	for _, invalid := range []string{path + "-1", path, "/api/v1/admin/checker-stats/?round=4"} {
		if res := performRequest(router, http.MethodGet, invalid, "", headers); res.Code != http.StatusBadRequest {
			t.Errorf("invalid filters %q returned %d", invalid, res.Code)
		}
	}
	if game.teamID != uuid.Nil() {
		t.Fatal("invalid or unauthenticated request reached the game service")
	}
	res := performRequest(router, http.MethodGet, path+"4", "", headers)
	var stats api.AdminCheckerStats
	if err := json.Unmarshal(res.Body.Bytes(), &stats); err != nil || res.Code != http.StatusOK {
		t.Fatalf("response %d: %s, err = %v", res.Code, res.Body.String(), err)
	}
	if game.teamID != httpTeamID || game.round != 4 || stats.TeamID != api.UUID(httpTeamID) || stats.Round != 4 ||
		len(stats.Tasks) != 1 || stats.Tasks[0].TaskID != api.UUID(httpTaskID) || stats.Tasks[0].Put.Failed != 1 ||
		stats.Tasks[0].Put.Maximum != 5 || stats.Tasks[0].Put.Reported != 2 {
		t.Fatalf("unexpected checker counts: %+v", stats)
	}
}

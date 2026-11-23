package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type adminMutationGame struct {
	GameService

	settings domain.GameSettingsUpdate
	teams    []uuid.UUID
	tasks    []uuid.UUID
	calls    int
}

func (g *adminMutationGame) Lifecycle(context.Context) (domain.Lifecycle, error) {
	return domain.Lifecycle{
		Config: domain.GameConfig{
			ID:           1,
			FlagLifetime: 5,
			RoundTime:    60,
			Rounds:       new(int32(300)),
			Mode:         "classic",
			Timezone:     "UTC",
			StartTime:    time.Date(2027, time.January, 1, 12, 0, 0, 0, time.UTC),
		},
	}, nil
}

func (g *adminMutationGame) UpdateGameSettings(ctx context.Context, input domain.GameSettingsUpdate) (domain.Lifecycle, error) {
	g.calls++
	g.settings = input
	life, err := g.Lifecycle(ctx)
	life.Config.Rounds = input.Rounds
	return life, err
}

func (g *adminMutationGame) DeactivateTeams(_ context.Context, ids []uuid.UUID) (int32, error) {
	g.calls++
	g.teams = ids
	return int32(len(ids)), nil
}

func (g *adminMutationGame) DeactivateTasks(_ context.Context, ids []uuid.UUID) (int32, error) {
	g.calls++
	g.tasks = ids
	return int32(len(ids)), nil
}

func TestAdminGameSettingsRouteAndCSRF(t *testing.T) {
	game := &adminMutationGame{}
	router := testRouter(t, "api", game, testAuth{}, &testFlags{})
	path := "/api/v1/admin/game/config/"
	if got := performRequest(router, http.MethodGet, path, "", nil).Code; got != http.StatusForbidden {
		t.Fatalf("anonymous settings: %d", got)
	}
	if got := performRequest(router, http.MethodGet, path, "", map[string]string{
		"Cookie": "session=valid",
	}); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"extra_checks":0`) {
		t.Fatalf("admin settings: %d %s", got.Code, got.Body.String())
	}
	body := `{"generation":0,"rounds":360,"flag_lifetime":5,"game_hardness":10,"inflation":true,"volga_attacks_mode":false,"round_time":60,"extra_checks":0,"mode":"classic","timezone":"UTC","start_time":"2027-01-01T12:00:00Z"}`
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"anonymous", nil, http.StatusForbidden},
		{"missing CSRF", map[string]string{
			"Cookie": "session=valid",
		}, http.StatusForbidden},
		{"valid", map[string]string{
			"Cookie":       "session=valid",
			"X-CSRF-Token": strings.Repeat("c", 64),
		}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := performRequest(router, http.MethodPut, path, body, tc.headers)
			if got.Code != tc.want {
				t.Fatalf("settings response: %d %s", got.Code, got.Body.String())
			}
		})
	}
	if game.calls != 1 || game.settings.Rounds == nil || *game.settings.Rounds != 360 || game.settings.Generation != 0 {
		t.Fatalf("settings request: calls=%d input=%+v", game.calls, game.settings)
	}
}

func TestAdminBulkDeactivationRoutes(t *testing.T) {
	game := &adminMutationGame{}
	router := testRouter(t, "api", game, testAuth{}, &testFlags{})
	headers := map[string]string{
		"Cookie":       "session=valid",
		"X-CSRF-Token": strings.Repeat("c", 64),
	}
	teamIDs := []uuid.UUID{
		httpTeamID,
		uuid.MustParse("01912345-6789-7000-8000-000000000022"),
	}
	taskIDs := []uuid.UUID{
		httpTaskID,
		uuid.MustParse("01912345-6789-7000-8000-000000000032"),
	}
	for _, tc := range []struct {
		path string
		want []uuid.UUID
	}{
		{"/api/v1/admin/teams/deactivate/", teamIDs},
		{"/api/v1/admin/tasks/deactivate/", taskIDs},
	} {
		body, err := json.Marshal(map[string][]uuid.UUID{
			"ids": tc.want,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := performRequest(router, http.MethodPost, tc.path, string(body), map[string]string{
			"Cookie": "session=valid",
		}); got.Code != http.StatusForbidden {
			t.Fatalf("CSRF guard %s: %d %s", tc.path, got.Code, got.Body.String())
		}
		got := performRequest(router, http.MethodPost, tc.path, string(body), headers)
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"deactivated":2`) {
			t.Fatalf("deactivation %s: %d %s", tc.path, got.Code, got.Body.String())
		}
	}
	if got := game.teams; !slices.Equal(got, teamIDs) {
		t.Fatalf("team ids: got %v, want %v", got, teamIDs)
	}
	if got := game.tasks; !slices.Equal(got, taskIDs) {
		t.Fatalf("task ids: got %v, want %v", got, taskIDs)
	}
	if game.calls != 2 {
		t.Fatalf("batch calls: %d, want 2", game.calls)
	}
}

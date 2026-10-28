package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type checkerLogsGame struct {
	GameService

	page      domain.CheckerLogPage
	beforeID  *uuid.UUID
	requested int32
	called    bool
}

func (g *checkerLogsGame) CheckerLogs(_ context.Context, teamID, taskID uuid.UUID, beforeID *uuid.UUID, limit int32) (domain.CheckerLogPage, error) {
	g.called, g.beforeID, g.requested = true, beforeID, limit
	g.page.TeamID, g.page.TaskID = teamID, taskID
	return g.page, nil
}

func TestGetClientCheckerLogsReturnsOnlyPublicLogFields(t *testing.T) {
	teamID := uuid.MustParse("01912345-6789-7000-8000-000000000001")
	taskID := uuid.MustParse("01912345-6789-7000-8000-000000000002")
	beforeID := uuid.MustParse("01912345-6789-7000-8000-000000000003")
	logID := uuid.MustParse("01912345-6789-7000-8000-000000000004")
	game := &checkerLogsGame{page: domain.CheckerLogPage{
		Limit: 100, HasMore: true, NextBeforeID: &beforeID,
		Logs: []domain.CheckerLog{{ID: logID, Round: 4, Action: "put", Stage: "classic", Status: 101, PublicMessage: "stored"}},
	}}
	handler := NewHandler(game, nil, nil, nil)
	response, err := handler.GetClientCheckerLogs(context.Background(), api.GetClientCheckerLogsParams{
		TeamID: teamID, TaskID: taskID,
		Limit: api.NewOptInt32(100), BeforeID: api.NewOptString(beforeID.String()),
	})
	if err != nil {
		t.Fatal(err)
	}
	page, ok := response.(*api.CheckerLogPage)
	if !ok {
		t.Fatalf("response type = %T, want *api.CheckerLogPage", response)
	}
	if !game.called || game.requested != 100 || game.beforeID == nil || *game.beforeID != beforeID {
		t.Fatalf("service call = called %t, limit %d, before %v", game.called, game.requested, game.beforeID)
	}
	if page.TeamID != api.UUID(teamID) || page.TaskID != api.UUID(taskID) || !page.HasMore || len(page.Logs) != 1 ||
		!page.NextBeforeID.IsSet() || page.NextBeforeID.Value != api.UUID(beforeID) || page.Logs[0].ID != api.UUID(logID) {
		t.Fatalf("public checker log page = %+v", page)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, privateField := range []string{"private_message", "command", "flag", "private_data"} {
		if strings.Contains(string(encoded), privateField) {
			t.Fatalf("public response contains %q: %s", privateField, encoded)
		}
	}
}

func TestGetClientCheckerLogsRejectsInvalidCursor(t *testing.T) {
	game := &checkerLogsGame{}
	handler := NewHandler(game, nil, nil, nil)
	response, err := handler.GetClientCheckerLogs(context.Background(), api.GetClientCheckerLogsParams{
		TeamID: httpTeamID, TaskID: httpTaskID,
		BeforeID: api.NewOptString("not-a-uuid"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(*api.GetClientCheckerLogsBadRequest); !ok || game.called {
		t.Fatalf("invalid cursor response = %T, service called %t", response, game.called)
	}
}

func TestClientCheckerLogsRouteEncodesPublicPage(t *testing.T) {
	teamID := uuid.MustParse("01912345-6789-7000-8000-000000000001")
	taskID := uuid.MustParse("01912345-6789-7000-8000-000000000002")
	game := &checkerLogsGame{page: domain.CheckerLogPage{
		TeamID: teamID, TaskID: taskID, Limit: 100,
		Logs: []domain.CheckerLog{{Action: "check", Stage: "classic", PublicMessage: "UP"}},
	}}
	handler := NewHandler(game, nil, nil, nil)
	server, err := api.NewServer(handler, handler, handler)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/client/teams/" + teamID.String() + "/tasks/" + taskID.String() + "/logs/?limit=100"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("checker logs route status = %d, body %s", response.Code, response.Body.String())
	}
	for _, privateField := range []string{"private_message", "command", "flag", "private_data"} {
		if strings.Contains(response.Body.String(), privateField) {
			t.Fatalf("checker logs route exposes %q: %s", privateField, response.Body.String())
		}
	}
}

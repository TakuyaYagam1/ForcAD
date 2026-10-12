package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/config"
	httpv1 "github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/auth"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/flags"
	"github.com/prometheus/client_golang/prometheus"
)

type testAuth struct{ err error }

func (a testAuth) Login(_ context.Context, user, password string) (string, error) {
	if user != "admin" || password != "password" {
		return "", auth.ErrUnauthorized
	}
	return "session-value", a.err
}
func (a testAuth) Status(_ context.Context, token string) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	if token != "valid" {
		return "", auth.ErrUnauthorized
	}
	return "admin", nil
}
func (a testAuth) Logout(context.Context, string) error { return a.err }

type testFlags struct {
	entered chan struct{}
	release chan struct{}
	err     error
	calls   int
}

func (f *testFlags) Submit(ctx context.Context, token string, values []string) ([]flags.Response, error) {
	f.calls++
	if f.entered != nil {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if token != "0123456789abcdef" {
		return nil, domain.ErrTeamTokenInvalid
	}
	if f.err != nil {
		return nil, f.err
	}
	result := make([]flags.Response, 0, len(values))
	for _, value := range values {
		result = append(result, flags.Response{Flag: value, Message: "invalid"})
	}
	return result, nil
}

var (
	httpTeamID = uuid.MustParse("01912345-6789-7000-8000-000000000001")
	httpTaskID = uuid.MustParse("01912345-6789-7000-8000-000000000002")
)

type testGame struct {
	httpv1.GameService

	changed     string
	historyTeam uuid.UUID
	historyTask *uuid.UUID
}

func (*testGame) Teams(context.Context, bool) ([]domain.Team, error) {
	return []domain.Team{{ID: httpTeamID, Name: "team", IP: "127.0.0.1", Token: "secret-team-token", Active: true}}, nil
}
func (g *testGame) ChangeState(_ context.Context, action string, _ *int64) error {
	g.changed = action
	return nil
}
func (*testGame) Runtime(context.Context) (domain.RuntimeStatus, error) {
	return domain.RuntimeStatus{Phase: "running", Generation: 1}, nil
}
func (g *testGame) History(_ context.Context, teamID uuid.UUID, taskID *uuid.UUID) ([]domain.Cell, error) {
	g.historyTeam, g.historyTask = teamID, taskID
	return []domain.Cell{{ID: uuid.NewV7(), TeamID: httpTeamID, TaskID: httpTaskID, PublicMessage: "UP", PrivateMessage: "private-marker", Command: "secret-command", Timestamp: "1-0"}}, nil
}
func (*testGame) PublicHistory(context.Context, uuid.UUID) ([]domain.PublicCell, error) {
	return []domain.PublicCell{{TeamID: httpTeamID, TaskID: httpTaskID, Message: "UP", Timestamp: "1-0"}}, nil
}

type testLimiter struct{}

func (testLimiter) Allow(context.Context, string, int, int, int) (bool, error) { return true, nil }

func testRouter(t *testing.T, role string, g httpv1.GameService, a httpv1.AuthService, f httpv1.FlagService) http.Handler {
	t.Helper()
	cfg := config.Config{Role: role, HTTPMaxInflight: 1, RequestTimeout: time.Second}
	noop := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r, err := NewRouter(cfg, httpv1.NewHandler(g, a, f), noop, noop, prometheus.NewRegistry(), testLimiter{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTeamRoutesUseUUIDs(t *testing.T) {
	router := testRouter(t, "api", &testGame{}, testAuth{}, &testFlags{})
	headers := map[string]string{"Cookie": "session=valid"}
	path := "/api/admin/teams/" + httpTeamID.String() + "/"
	response := request(router, http.MethodGet, path, "", headers)
	if response.Code != http.StatusOK {
		t.Fatalf("team response: %d %s", response.Code, response.Body.String())
	}
	var team struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &team); err != nil || team.ID != httpTeamID.String() {
		t.Fatalf("team UUID: %q, decode error: %v", team.ID, err)
	}
	for _, id := range []string{"1", "not-a-uuid", "01912345-6789-7000-8000-00000000000z"} {
		response = request(router, http.MethodGet, "/api/admin/teams/"+id+"/", "", headers)
		if response.Code != http.StatusBadRequest {
			t.Errorf("invalid ID %q: %d %s", id, response.Code, response.Body.String())
		}
	}
	response = request(router, http.MethodGet, "/api/admin/teams/"+uuid.NewV7().String()+"/", "", headers)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown UUID response: %d %s", response.Code, response.Body.String())
	}
}

func TestAdminHistoryFiltersUseUUIDs(t *testing.T) {
	game := &testGame{}
	router := testRouter(t, "api", game, testAuth{}, &testFlags{})
	headers := map[string]string{"Cookie": "session=valid"}
	path := "/api/admin/teamtasks/?team_id=" + httpTeamID.String() + "&task_id=" + httpTaskID.String()
	response := request(router, http.MethodGet, path, "", headers)
	if response.Code != http.StatusOK {
		t.Fatalf("history response: %d %s", response.Code, response.Body.String())
	}
	if game.historyTeam != httpTeamID || game.historyTask == nil || *game.historyTask != httpTaskID {
		t.Fatalf("history filters: team=%s task=%v", game.historyTeam, game.historyTask)
	}
	var entries []struct {
		ID     uuid.UUID `json:"id"`
		TeamID uuid.UUID `json:"team_id"`
		TaskID uuid.UUID `json:"task_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID == uuid.Nil() || entries[0].TeamID != httpTeamID || entries[0].TaskID != httpTaskID {
		t.Fatalf("history UUIDs: %+v", entries)
	}
	response = request(router, http.MethodGet, "/api/admin/teamtasks/?team_id=1&task_id=2", "", headers)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("numeric history filters: %d %s", response.Code, response.Body.String())
	}
}
func request(r http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestFlagsAliasesValidateBeforeUsecase(t *testing.T) {
	f := &testFlags{}
	r := testRouter(t, "receiver", &testGame{}, testAuth{}, f)
	headers := map[string]string{"X-Team-Token": "0123456789abcdef"}
	values := make([]string, 100)
	for i := range values {
		values[i] = "invalid"
	}
	batch, _ := json.Marshal(values)
	for _, path := range []string{"/flags", "/flags/"} {
		w := request(r, "PUT", path, string(batch), headers)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var got []map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got) != 100 {
			t.Fatalf("batch response: %v %s", err, w.Body.String())
		}
	}
	before := f.calls
	for _, body := range []string{`[null]`, `[1]`, `{"flags":[]}`, `[] []`, `["` + strings.Repeat("a", 33) + `"]`} {
		w := request(r, "PUT", "/flags", body, headers)
		if w.Code != 400 {
			t.Fatalf("invalid batch %q: %d %s", body, w.Code, w.Body.String())
		}
	}
	valuesWithExtra := make([]string, 0, len(values)+1)
	valuesWithExtra = append(valuesWithExtra, values...)
	valuesWithExtra = append(valuesWithExtra, "extra")
	batch, _ = json.Marshal(valuesWithExtra)
	if w := request(r, "PUT", "/flags", string(batch), headers); w.Code != 400 {
		t.Fatalf("101 flags: %d", w.Code)
	}
	if f.calls != before {
		t.Fatal("invalid payload reached usecase")
	}
	if w := request(r, "PUT", "/flags", "[]", nil); w.Code != 400 {
		t.Fatalf("missing token: %d", w.Code)
	}
	if w := request(r, "PUT", "/flags", strings.Repeat(" ", 40<<10), headers); w.Code != 413 {
		t.Fatalf("body limit: %d", w.Code)
	}
}

func TestAdminAuthenticationAndOrigin(t *testing.T) {
	r := testRouter(t, "api", &testGame{}, testAuth{}, &testFlags{})
	if w := request(r, "GET", "/api/admin/teams/", "", nil); w.Code != 403 {
		t.Fatalf("anonymous admin: %d", w.Code)
	}
	if w := request(r, "GET", "/api/admin/teams/", "", map[string]string{"Cookie": "session=valid"}); w.Code != 200 {
		t.Fatalf("authenticated admin: %d", w.Code)
	}
	body := `{"username":"admin","password":"password"}`
	w := request(r, "POST", "/api/admin/login/", body, map[string]string{"X-Forwarded-Proto": "https"})
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %v", cookies)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("admin response is cacheable")
	}
	if w := request(r, "POST", "/api/admin/login/", body, map[string]string{"Origin": "https://other.example"}); w.Code != 403 {
		t.Fatalf("cross origin: %d", w.Code)
	}
	if w := request(r, "POST", "/api/admin/login/", body, map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != 403 {
		t.Fatalf("cross site: %d", w.Code)
	}
}

func TestAdminDependencyFailureIsNotInvalidSession(t *testing.T) {
	r := testRouter(t, "api", &testGame{}, testAuth{err: errors.New("cache-secret-connection-failure")}, &testFlags{})
	w := request(r, "GET", "/api/admin/teams/", "", map[string]string{"Cookie": "session=valid"})
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("dependency error: %d %s", w.Code, w.Body.String())
	}
}

func TestErrorHandlerPreservesWrappedPublicErrors(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	handle := errorHandler(logger)
	tests := []struct {
		name     string
		err      error
		status   int
		message  string
		mustHide string
	}{
		{
			name:     "game state sentinel",
			err:      fmt.Errorf("flags.Service.Submit - game has not started: %w", flags.ErrNotStarted),
			status:   http.StatusBadRequest,
			message:  flags.ErrNotStarted.Error(),
			mustHide: "flags.Service.Submit",
		},
		{
			name:     "batch sentinel",
			err:      fmt.Errorf("flags.Service.Submit - validate flag batch: %w", flags.ErrInvalidBatch),
			status:   http.StatusBadRequest,
			message:  flags.ErrInvalidBatch.Error(),
			mustHide: "flags.Service.Submit",
		},
		{
			name:     "validation error",
			err:      fmt.Errorf("game.Service.SaveTask - invalid task settings: %w", &domain.ValidationError{Message: "Invalid task settings"}),
			status:   http.StatusBadRequest,
			message:  "Invalid task settings",
			mustHide: "game.Service.SaveTask",
		},
		{
			name:     "state error",
			err:      fmt.Errorf("game.Service.ChangeState - reject state change: %w", &domain.StateError{Message: "Game has already finished"}),
			status:   http.StatusConflict,
			message:  "Game has already finished",
			mustHide: "game.Service.ChangeState",
		},
		{
			name:     "unexpected error",
			err:      fmt.Errorf("postgres.Repository.Get - query failed: %w", errors.New("postgres://private-dsn")),
			status:   http.StatusServiceUnavailable,
			message:  "Service temporarily unavailable",
			mustHide: "private-dsn",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/admin/teams/", nil)
			response := httptest.NewRecorder()
			handle(req.Context(), response, req, test.err)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("response: %d %s", response.Code, response.Body.String())
			}
			if test.status == http.StatusServiceUnavailable && response.Header().Get("Retry-After") == "" {
				t.Fatal("temporary failure has no retry delay")
			}
			if strings.Contains(response.Body.String(), test.mustHide) {
				t.Fatalf("response leaked internal error context: %s", response.Body.String())
			}
		})
	}
}

func TestWrappedFlagSentinelsKeepPublicMessages(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "not started", err: flags.ErrNotStarted},
		{name: "invalid batch", err: flags.ErrInvalidBatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			serviceErr := fmt.Errorf("flags.Service.Submit - operation failed: %w", test.err)
			r := testRouter(t, "receiver", &testGame{}, testAuth{}, &testFlags{err: serviceErr})
			w := request(r, "PUT", "/flags", "[]", map[string]string{"X-Team-Token": "0123456789abcdef"})
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), test.err.Error()) {
				t.Fatalf("public response: %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "flags.Service.Submit") {
				t.Fatalf("response exposed internal location: %s", w.Body.String())
			}
		})
	}
}

func TestPublicResponsesExcludePrivateData(t *testing.T) {
	r := testRouter(t, "api", &testGame{}, testAuth{}, &testFlags{})
	for _, path := range []string{"/api/client/teams/", "/api/client/teams/" + httpTeamID.String() + "/"} {
		w := request(r, "GET", path, "", nil)
		if w.Code != 200 {
			t.Fatalf("public route: %d %s", w.Code, w.Body.String())
		}
		for _, secret := range []string{"secret-team-token", "private-marker", "secret-command", "private_message", "token"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("public route leaked %s", secret)
			}
		}
	}
}

func TestPublicHistoryUsesSnapshotIdentity(t *testing.T) {
	router := testRouter(t, "api", &testGame{}, testAuth{}, &testFlags{})
	response := request(router, http.MethodGet, "/api/client/teams/"+httpTeamID.String()+"/", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("history response: %d %s", response.Code, response.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("history response: %s, decode error: %v", response.Body.String(), err)
	}
	if _, hasLogID := rows[0]["id"]; hasLogID {
		t.Fatal("public round snapshot must not invent a checker log ID")
	}
	if rows[0]["team_id"] != httpTeamID.String() || rows[0]["task_id"] != httpTaskID.String() {
		t.Fatalf("snapshot entity IDs: %+v", rows[0])
	}
}

func TestGameActionRequiresConfirmation(t *testing.T) {
	g := &testGame{}
	r := testRouter(t, "api", g, testAuth{}, &testFlags{})
	headers := map[string]string{"Cookie": "session=valid"}
	if w := request(r, "POST", "/api/admin/game/finish/", `{}`, headers); w.Code != 400 || g.changed != "" {
		t.Fatalf("missing confirmation: %d", w.Code)
	}
	if w := request(r, "POST", "/api/admin/game/finish/", `{"confirm":true,"generation":1.5}`, headers); w.Code != 400 || g.changed != "" {
		t.Fatalf("fractional generation: %d", w.Code)
	}
	if w := request(r, "POST", "/api/admin/game/finish/", `{"confirm":true}`, headers); w.Code != 200 || g.changed != "finish" {
		t.Fatalf("confirmed finish: %d %s", w.Code, w.Body.String())
	}
}

func TestCapacityDoesNotQueueRequestsOrBlockHealth(t *testing.T) {
	f := &testFlags{entered: make(chan struct{}), release: make(chan struct{})}
	r := testRouter(t, "receiver", &testGame{}, testAuth{}, f)
	headers := map[string]string{"X-Team-Token": "0123456789abcdef"}
	var wg sync.WaitGroup
	wg.Go(func() { request(r, "PUT", "/flags", "[]", headers) })
	<-f.entered
	defer func() { close(f.release); wg.Wait() }()
	if w := request(r, "PUT", "/flags", "[]", headers); w.Code != 503 {
		t.Fatalf("saturated receiver: %d", w.Code)
	}
	if w := request(r, "GET", "/flags/health/", "", nil); w.Code != 200 {
		t.Fatalf("health blocked: %d", w.Code)
	}
	if w := request(r, "GET", "/api/admin/teams/", "", nil); w.Code != 503 && w.Code != 404 {
		t.Fatalf("receiver exposes admin: %d", w.Code)
	}
}

func TestRoleSeparatesReceiverAndAPI(t *testing.T) {
	for _, tc := range []struct{ role, path, method string }{{"receiver", "/api/admin/teams/", "GET"}, {"receiver", "/api/client/teams/", "GET"}, {"api", "/flags", "PUT"}} {
		r := testRouter(t, tc.role, &testGame{}, testAuth{}, &testFlags{})
		if w := request(r, tc.method, tc.path, "", nil); w.Code != 404 {
			t.Fatalf("%s exposes %s: %d", tc.role, tc.path, w.Code)
		}
	}
}

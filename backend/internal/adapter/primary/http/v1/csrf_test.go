package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminMutationsRequireSessionCSRF(t *testing.T) {
	game := &testGame{}
	router := testRouter(t, "api", game, testAuth{}, &testFlags{})
	for _, path := range []string{"/api/admin/game/pause/", "/api/admin/teams/", "/api/admin/tasks/", "/api/admin/events/", "/api/admin/logout/"} {
		for _, token := range []string{"", "invalid", strings.Repeat("b", 64)} {
			headers := map[string]string{"Cookie": "session=valid", "X-CSRF-Token": token}
			response := request(router, http.MethodPost, path, `{}`, headers)
			if response.Code != http.StatusForbidden {
				t.Fatalf("missing/wrong token for %s: %d", path, response.Code)
			}
		}
	}
	if game.changed != "" {
		t.Fatal("rejected CSRF request changed game")
	}
	headers := map[string]string{"Cookie": "session=valid", "X-CSRF-Token": strings.Repeat("c", 64)}
	if response := request(router, http.MethodPost, "/api/admin/game/pause/", `{}`, headers); response.Code != http.StatusOK || game.changed != "pause" {
		t.Fatalf("headerless CLI with valid CSRF rejected: %d", response.Code)
	}
	if response := request(router, http.MethodPost, "/api/admin/logout/", `{}`, headers); response.Code != http.StatusOK {
		t.Fatalf("authenticated logout rejected: %d", response.Code)
	}
	if response := request(router, http.MethodPost, "/api/admin/logout/", `{}`, nil); response.Code != http.StatusForbidden {
		t.Fatal("logout bypassed authenticated session")
	}
}

func TestLoginAndSessionExposeCSRFOnlyInNoncacheableJSON(t *testing.T) {
	router := testRouter(t, "api", &testGame{}, testAuth{}, &testFlags{})
	for _, fixture := range []struct {
		method, path, body string
		headers            map[string]string
	}{
		{http.MethodPost, "/api/admin/login/", `{"username":"admin","password":"password"}`, nil},
		{http.MethodGet, "/api/admin/status/", "", map[string]string{"Cookie": "session=valid"}},
	} {
		response := request(router, fixture.method, fixture.path, fixture.body, fixture.headers)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("session response %d cache=%s", response.Code, response.Header().Get("Cache-Control"))
		}
		var data struct {
			CSRF string `json:"csrf_token"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil || data.CSRF != strings.Repeat("c", 64) {
			t.Fatal("session response did not include its CSRF token")
		}
		if strings.Contains(response.Header().Get("Set-Cookie"), data.CSRF) {
			t.Fatal("synchronizer token leaked into cookie")
		}
	}
}

func TestLoginRejectsSimpleContentTypesAndUntrustedOrigins(t *testing.T) {
	router := testRouter(t, "api", &testGame{}, testAuth{}, &testFlags{})
	body := `{"username":"admin","password":"password"}`
	for _, kind := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
		response := request(router, http.MethodPost, "/api/admin/login/", body, map[string]string{"Content-Type": kind})
		if response.Code != http.StatusUnsupportedMediaType || response.Header().Get("Set-Cookie") != "" {
			t.Fatalf("login accepted simple content type %s: %d", kind, response.Code)
		}
	}
	for _, headers := range []map[string]string{
		{"Origin": "null"}, {"Origin": "/relative"}, {"Origin": "https://other.example"},
		{"Sec-Fetch-Site": "same-site"}, {"Sec-Fetch-Site": "cross-site"},
		{"Referer": "https://other.example/page"},
	} {
		if response := request(router, http.MethodPost, "/api/admin/login/", body, headers); response.Code != http.StatusForbidden {
			t.Fatalf("untrusted login headers accepted: %d", response.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/game/pause/", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session=valid")
	req.Header.Add("X-CSRF-Token", strings.Repeat("c", 64))
	req.Header.Add("X-CSRF-Token", strings.Repeat("c", 64))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatal("ambiguous duplicate CSRF header accepted")
	}
}

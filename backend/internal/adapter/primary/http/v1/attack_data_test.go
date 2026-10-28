package v1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

type attackDataGame struct {
	GameService

	data     map[string]map[string][]string
	err      error
	services []*string
}

func (g *attackDataGame) AttackData(_ context.Context, service *string) (map[string]map[string][]string, error) {
	g.services = append(g.services, service)
	if service == nil {
		return g.data, g.err
	}
	filtered := make(map[string]map[string][]string, 1)
	if data, ok := g.data[*service]; ok {
		filtered[*service] = data
	}
	return filtered, g.err
}

func TestAttackDataResponseSupportsConditionalGet(t *testing.T) {
	game := &attackDataGame{data: map[string]map[string][]string{
		"service-one": {"192.0.2.1": {"first-flag", "second-flag"}},
	}}
	first := httptest.NewRecorder()
	if err := writeAttackData(t.Context(), game, nil, "", first); err != nil {
		t.Fatal(err)
	}
	if first.Code != http.StatusOK || first.Body.String() != `{"service-one":{"192.0.2.1":["first-flag","second-flag"]}}` {
		t.Fatalf("full response status=%d body=%s", first.Code, first.Body.String())
	}
	if first.Header().Get("ETag") == "" || first.Header().Get("Cache-Control") != attackDataCacheControl || first.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("full response headers = %v", first.Header())
	}

	service := "service-one"
	second := httptest.NewRecorder()
	if err := writeAttackData(t.Context(), game, &service, `W/"other", W/`+first.Header().Get("ETag"), second); err != nil {
		t.Fatal(err)
	}
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional response status=%d body=%q", second.Code, second.Body.String())
	}
	if second.Header().Get("ETag") != first.Header().Get("ETag") || second.Header().Get("Cache-Control") != attackDataCacheControl {
		t.Fatalf("conditional response headers = %v", second.Header())
	}
	if len(game.services) != 2 || game.services[0] != nil || game.services[1] == nil || *game.services[1] != service {
		t.Fatalf("service filters = %v", game.services)
	}
}

func TestAttackDataResponseChangesETagWithRepresentation(t *testing.T) {
	game := &attackDataGame{data: map[string]map[string][]string{}}
	first := httptest.NewRecorder()
	if err := writeAttackData(t.Context(), game, nil, "", first); err != nil {
		t.Fatal(err)
	}
	game.data = map[string]map[string][]string{"service-one": {}}
	second := httptest.NewRecorder()
	if err := writeAttackData(t.Context(), game, nil, first.Header().Get("ETag"), second); err != nil {
		t.Fatal(err)
	}
	if second.Code != http.StatusOK || second.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatalf("changed response status=%d etag=%q, previous=%q", second.Code, second.Header().Get("ETag"), first.Header().Get("ETag"))
	}
}

func TestAttackDataResponsePropagatesLoadFailure(t *testing.T) {
	want := errors.New("attack data unavailable")
	game := &attackDataGame{err: want}
	if err := writeAttackData(t.Context(), game, nil, "", httptest.NewRecorder()); !errors.Is(err, want) {
		t.Fatalf("writeAttackData() error = %v, want %v", err, want)
	}
}

func TestAttackDataEndpointReadsParameters(t *testing.T) {
	game := &attackDataGame{data: map[string]map[string][]string{
		"service-one": {"192.0.2.1": {"flag"}},
		"service-two": {"192.0.2.2": {"other-flag"}},
	}}
	handler := NewHandler(game, nil, nil, nil)
	server, err := api.NewServer(handler, handler, handler)
	if err != nil {
		t.Fatal(err)
	}

	full := httptest.NewRecorder()
	server.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/api/client/attack_data/", nil))
	if full.Code != http.StatusOK || full.Body.String() != `{"service-one":{"192.0.2.1":["flag"]},"service-two":{"192.0.2.2":["other-flag"]}}` {
		t.Fatalf("full endpoint response status=%d body=%s", full.Code, full.Body.String())
	}

	filtered := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/client/attack_data/?service=service-one", nil)
	server.ServeHTTP(filtered, request)
	if filtered.Code != http.StatusOK || filtered.Body.String() != `{"service-one":{"192.0.2.1":["flag"]}}` {
		t.Fatalf("filtered endpoint response status=%d body=%s", filtered.Code, filtered.Body.String())
	}

	conditional := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/client/attack_data/?service=service-one", nil)
	request.Header.Set("If-None-Match", filtered.Header().Get("ETag"))
	server.ServeHTTP(conditional, request)
	if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 {
		t.Fatalf("conditional endpoint response status=%d body=%q", conditional.Code, conditional.Body.String())
	}
	if len(game.services) != 3 || game.services[0] != nil || game.services[1] == nil || *game.services[1] != "service-one" || game.services[2] == nil || *game.services[2] != "service-one" {
		t.Fatalf("endpoint service filters = %v", game.services)
	}

	unknown := httptest.NewRecorder()
	server.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/api/client/attack_data/?service=missing", nil))
	if unknown.Code != http.StatusOK || unknown.Body.String() != `{}` {
		t.Fatalf("unknown endpoint response status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	empty := httptest.NewRecorder()
	server.ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/api/client/attack_data/?service=", nil))
	if empty.Code != http.StatusOK || empty.Body.String() != `{}` {
		t.Fatalf("empty service response status=%d body=%s", empty.Code, empty.Body.String())
	}
}

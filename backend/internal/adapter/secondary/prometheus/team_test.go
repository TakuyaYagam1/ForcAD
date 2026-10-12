package prometheus

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

var trafficTeamID = uuid.MustParse("00000000-0000-7000-8000-000000000001")

type teamReader struct{ err error }

func (r *teamReader) Teams(context.Context, bool) ([]domain.Team, error) {
	return []domain.Team{{ID: trafficTeamID, Name: "Team 1", IP: "192.0.2.1", Token: "never-export-this-token"}}, r.err
}

func newTraffic(t *testing.T) (*TeamTraffic, *prometheus.Registry, *teamReader) {
	t.Helper()
	registry := prometheus.NewRegistry()
	reader := &teamReader{}
	metrics, err := NewTeamTraffic(registry, reader, []config.Team{{IP: "192.0.2.1", TrafficCIDRs: []string{"192.0.2.1/32", "2001:db8:1::/64"}}}, []string{"10.0.0.0/8"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	return metrics, registry, reader
}

func TestTrafficAttributionRejectsUntrustedForwarding(t *testing.T) {
	metrics, _, _ := newTraffic(t)
	handler := metrics.Middleware(func(*http.Request) string { return "/api/client/status/" })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for _, peer := range []string{"198.51.100.1:1234", "10.0.0.2:1234"} {
		request := httptest.NewRequest(http.MethodGet, "/api/client/status/", nil)
		request.RemoteAddr = peer
		request.Header.Set("X-Forwarded-For", "192.0.2.1")
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	for _, id := range []string{unknownTeam, trafficTeamID.String()} {
		if got := metricValue(metrics.requests.WithLabelValues(id, "/api/client/status/", "200")); got != 1 {
			t.Fatalf("%s requests: %v", id, got)
		}
	}
	if metrics.source("2001:db8:1::42") != trafficTeamID.String() {
		t.Fatal("IPv6 attribution failed")
	}
}

func TestVerifiedTeamOverridesSourceAndLabelsStayBounded(t *testing.T) {
	metrics, registry, _ := newTraffic(t)
	handler := metrics.Middleware(func(*http.Request) string { return "unmatched" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.ResolveTeam(r.Context(), trafficTeamID)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	request := httptest.NewRequest(http.MethodPut, "/attacker-controlled-path", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got := metricValue(metrics.requests.WithLabelValues(trafficTeamID.String(), "unmatched", "429")); got != 1 {
		t.Fatal(got)
	}
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, secret := range []string{"attacker-controlled-path", "never-export-this-token"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("metrics leaked %q", secret)
		}
	}
}

func TestWebSocketGaugeBalancesAndDoesNotPolluteRESTLatency(t *testing.T) {
	metrics, _, _ := newTraffic(t)
	handler := metrics.Middleware(func(*http.Request) string { t.Fatal("websocket classified as REST"); return "" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusSwitchingProtocols)
		closed := metrics.WebSocketOpened(r.Context())
		if got := metricValue(metrics.sockets.WithLabelValues(trafficTeamID.String())); got != 1 {
			t.Fatal(got)
		}
		closed()
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/events/ws", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got := metricValue(metrics.sockets.WithLabelValues(trafficTeamID.String())); got != 0 {
		t.Fatal(got)
	}
}

func TestRejectedWebSocketHandshakeIsCounted(t *testing.T) {
	metrics, _, _ := newTraffic(t)
	handler := metrics.Middleware(func(*http.Request) string { return "/api/events/ws" })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/events/ws", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got := metricValue(metrics.requests.WithLabelValues(trafficTeamID.String(), "/api/events/ws", "503")); got != 1 {
		t.Fatalf("rejected handshake count: %v", got)
	}
}

func TestTrafficRefreshFailurePreservesInventory(t *testing.T) {
	metrics, _, reader := newTraffic(t)
	reader.err = errors.New("database unavailable")
	if err := metrics.Refresh(t.Context()); !errors.Is(err, reader.err) {
		t.Fatal(err)
	}
	if metrics.source("192.0.2.1") != trafficTeamID.String() {
		t.Fatal("last good inventory lost")
	}
}

func TestTrafficConcurrentRefreshAndRequests(t *testing.T) {
	metrics, _, _ := newTraffic(t)
	var workers sync.WaitGroup
	for range 5 {
		workers.Go(func() {
			for range 20 {
				if err := metrics.Refresh(t.Context()); err != nil {
					t.Error(err)
				}
				if metrics.source("192.0.2.1") != trafficTeamID.String() {
					t.Error("identity lost")
				}
				metrics.ObserveFlags(trafficTeamID, "duplicate", 100)
			}
		})
	}
	workers.Wait()
}

func metricValue(collector prometheus.Collector) float64 {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	families, err := registry.Gather()
	if err != nil || len(families) != 1 || len(families[0].Metric) != 1 {
		panic("unexpected metric collection")
	}
	metric := families[0].Metric[0]
	if metric.Counter != nil {
		return metric.Counter.GetValue()
	}
	return metric.Gauge.GetValue()
}

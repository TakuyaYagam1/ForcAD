package load_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"
)

func phaseTestClient(t *testing.T, timeout time.Duration) (*http.Client, *transportPhaseTransport) {
	t.Helper()
	base := &http.Transport{
		MaxConnsPerHost:     2,
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
	}
	phases := newTransportPhaseTransport(base)
	client := &http.Client{
		Transport: phases,
		Timeout:   timeout,
	}
	t.Cleanup(client.CloseIdleConnections)
	return client, phases
}

func TestTransportPhasesCaptureSlowHeadersAndConnectionReuse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if waitContext(r.Context(), 510*time.Millisecond) {
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	client, phases := phaseTestClient(t, 3*time.Second)
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/client/attack_data/?token=secret-query", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Vegeta-Seq", "537431")
		request.Header.Set("Authorization", "secret-header")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	report := phases.snapshot()
	if report.Seen != 2 || report.Completed != 2 || report.RetainedSlow != 2 || report.RetainedTransportErrors != 0 {
		t.Fatalf("incorrect slow-header counts: %+v", report)
	}
	for index, record := range report.Records {
		if record.Sequence != "537431" || record.Route != "/api/client/attack_data/" || record.Status != 200 || record.StartedUTC.IsZero() {
			t.Fatalf("request correlation was lost: %+v", record)
		}
		seen := make(map[string]bool)
		for _, event := range record.Events {
			seen[event.Phase] = true
			if event.Phase == "first_response_byte" && event.Milliseconds < 500 {
				t.Fatal("first-byte offset omitted the known server delay")
			}
		}
		for _, name := range []string{"get_conn", "got_conn", "wrote_request", "first_response_byte", "round_trip_done", "body_done"} {
			if !seen[name] {
				t.Errorf("missing phase %s", name)
			}
		}
		if index == 0 && (!seen["connect_start"] || !seen["connect_done"]) {
			t.Fatal("initial connection timing was not captured")
		}
	}
	var reused bool
	for _, event := range report.Records[1].Events {
		if event.Phase == "got_conn" {
			reused = event.Reused && event.WasIdle
		}
	}
	if !reused {
		t.Fatal("observer changed or failed to report idle connection reuse")
	}
	data, err := json.Marshal(report)
	if err != nil || strings.Contains(string(data), "secret") || strings.Contains(string(data), server.URL) {
		t.Fatalf("trace retained raw URL/header material: %s, %v", data, err)
	}
}

func TestTransportPhasesPreserveHeaderDeadlineAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	for _, canceled := range []bool{false, true} {
		client, phases := phaseTestClient(t, 40*time.Millisecond)
		ctx, cancel := context.WithCancel(t.Context())
		if canceled {
			cancel()
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/flags?token=secret", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		cancel()
		want, category := context.DeadlineExceeded, "deadline"
		if canceled {
			want, category = context.Canceled, "canceled"
		}
		if !errors.Is(err, want) {
			t.Fatalf("wrapper changed client error identity: %v", err)
		}
		report := phases.snapshot()
		if report.Completed != 1 || report.RetainedTransportErrors != 1 || report.Records[0].ErrorCategory != category {
			t.Fatalf("deadline/cancellation trace lost: %+v", report)
		}
	}
}

func TestTransportPhasesCaptureBodyReadDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client, phases := phaseTestClient(t, 50*time.Millisecond)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/client/status/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body wrapper changed deadline: %v", err)
	}
	report := phases.snapshot()
	if report.Completed != 1 || report.RetainedTransportErrors != 1 || report.Records[0].ErrorCategory != "deadline" || report.Records[0].Status != 200 {
		t.Fatalf("successful headers hid body-read timeout: %+v", report)
	}
}

func TestTransportPhaseErrorCapacityIsReservedWhenSlowCapacityFills(t *testing.T) {
	phases := newTransportPhaseTransport(http.DefaultTransport)
	phases.slowLimit, phases.errorLimit = 1, 1
	for range 3 {
		phases.retain(transportPhaseRecord{
			Milliseconds: 600,
		})
	}
	for range 2 {
		phases.retain(transportPhaseRecord{
			ErrorCategory: "deadline",
		})
	}
	report := phases.snapshot()
	if report.RetainedSlow != 1 || report.DroppedSlow != 2 || report.RetainedTransportErrors != 1 || report.DroppedTransportErrors != 1 || len(report.Records) != 2 {
		t.Fatalf("capacity was unbounded or errors lost their reserve: %+v", report)
	}
}

func TestTransportPhaseDeadlineSurvivesFullSlowAndHTTPErrorsBuckets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client, phases := phaseTestClient(t, 40*time.Millisecond)
	phases.slowLimit, phases.httpLimit = 1, 1
	for range 3 {
		phases.retain(transportPhaseRecord{
			Milliseconds: 600,
		})
		phases.retain(transportPhaseRecord{
			Status:        503,
			ErrorCategory: "http_5xx",
		})
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/client/attack_data/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Vegeta-Seq", "537431")
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline fixture changed: %v", err)
	}
	report := phases.snapshot()
	if report.RetainedSlow != 1 || report.DroppedSlow != 2 || report.RetainedHTTPErrors != 1 || report.DroppedHTTPErrors != 2 || report.RetainedTransportErrors != 1 || report.DroppedTransportErrors != 0 || len(report.Records) != 3 {
		t.Fatalf("HTTP errors or slow requests displaced a transport timeout: %+v", report)
	}
	record := report.Records[2]
	if record.ErrorCategory != "deadline" || record.Sequence != "537431" || record.Route != "/api/client/attack_data/" {
		t.Fatalf("retained timeout lost correlation: %+v", record)
	}
	seen := make(map[string]bool)
	for _, event := range record.Events {
		seen[event.Phase] = true
	}
	for _, name := range []string{"get_conn", "connect_start", "connect_done", "got_conn", "wrote_request", "round_trip_done"} {
		if !seen[name] {
			t.Errorf("retained timeout lost phase %s", name)
		}
	}
}

type phaseFixtureTransport struct {
	err    error
	closed bool
}

func (f *phaseFixtureTransport) CloseIdleConnections() {
	f.closed = true
}

func (f *phaseFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	trace := httptrace.ContextClientTrace(request.Context())
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			for range 50 {
				trace.GetConn("secret-address")
			}
		})
	}
	wait.Wait()
	return nil, f.err
}

func TestTransportPhasesBoundConcurrentHooksAndRedactUnknownValues(t *testing.T) {
	want := errors.New("secret raw dial error")
	base := &phaseFixtureTransport{
		err: want,
	}
	phases := newTransportPhaseTransport(base)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/private/secret?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Vegeta-Seq", "secret-header")
	response, err := phases.RoundTrip(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, want) {
		t.Fatal("transport wrapper changed the underlying error")
	}
	phases.CloseIdleConnections()
	if !base.closed {
		t.Fatal("wrapper suppressed idle connection cleanup")
	}
	report := phases.snapshot()
	record := report.Records[0]
	if record.Route != "other" || record.Sequence != "-" || len(record.Events) != 24 || record.DroppedEvents != 377 {
		t.Fatalf("unbounded or unnormalized event state: %+v", record)
	}
	data, err := json.Marshal(report)
	if err != nil || strings.Contains(string(data), "secret") {
		t.Fatalf("raw diagnostic values leaked: %s, %v", data, err)
	}
}

func TestAttackTransportPhasesAreDiagnosticOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	for _, settings := range []struct {
		diagnostics string
		phases      string
	}{
		{"", ""}, {"1", ""}, {"", "1"}, {"1", "1"},
	} {
		t.Setenv("LOAD_DIAGNOSTICS", settings.diagnostics)
		t.Setenv("LOAD_HTTP_PHASES", settings.phases)
		report := attack(t.Context(), attackOptions{
			rps:      100,
			duration: 50 * time.Millisecond,
			workers:  4,
			target:   readTarget(server.URL, "/api/client/status/"),
			validate: validateRead(false),
		})
		if settings.phases == "" {
			if report.TransportPhases != nil {
				t.Fatal("ordinary attack unexpectedly enabled the diagnostic observer")
			}
			continue
		}
		if report.TransportPhases == nil || report.TransportPhases.Seen != report.Requests || report.TransportPhases.Completed != report.Requests {
			t.Fatalf("diagnostic observer did not drain every Vegeta request: %+v", report.TransportPhases)
		}
	}
}

func TestTransportPhasesRetainFast5xxButNotExpected429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/flags" {
			w.WriteHeader(http.StatusTooManyRequests)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client, phases := phaseTestClient(t, 3*time.Second)
	for _, path := range []string{"/flags", "/api/client/attack_data/"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	report := phases.snapshot()
	if report.Seen != 2 || report.Completed != 2 || report.RetainedHTTPErrors != 1 || report.RetainedTransportErrors != 0 || report.RetainedSlow != 0 || len(report.Records) != 1 {
		t.Fatalf("fast5xx was lost or expected429 classified as an error: %+v", report)
	}
	if report.Records[0].Status != 503 || report.Records[0].ErrorCategory != "http_5xx" {
		t.Fatalf("5xx classification changed: %+v", report.Records[0])
	}
}

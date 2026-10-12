package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPServerHeaderValueLimit(t *testing.T) {
	var handled atomic.Int32
	testServer := httptest.NewTestServer(t, nil)
	testServer.Config = newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handled.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	if got := testServer.Config.MaxHeaderValueCount; got != 100 {
		t.Fatalf("MaxHeaderValueCount = %d, want 100", got)
	}
	if got := testServer.Config.MaxHeaderBytes; got != 16<<10 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", got, 16<<10)
	}
	if testServer.Config.ReadHeaderTimeout != 5*time.Second || testServer.Config.ReadTimeout != 15*time.Second || testServer.Config.IdleTimeout != time.Minute {
		t.Fatal("new HTTP server changed existing timeout settings")
	}

	client := testServer.Client()
	client.Timeout = 2 * time.Second
	requests := []struct {
		name    string
		path    string
		headers http.Header
	}{
		{
			name: "public flags",
			path: "/flags",
			headers: http.Header{
				"X-Team-Token":      {"0123456789abcdef"},
				"Content-Type":      {"application/json"},
				"Accept":            {"application/json"},
				"Origin":            {"https://game.example"},
				"X-Forwarded-For":   {"192.0.2.10"},
				"X-Forwarded-Proto": {"https"},
			},
		},
		{
			name: "control readiness",
			path: "/readyz",
			headers: http.Header{
				"Accept":            {"application/json"},
				"X-Forwarded-For":   {"192.0.2.10"},
				"X-Forwarded-Proto": {"https"},
			},
		},
		{
			name: "websocket upgrade headers",
			path: "/api/events/ws",
			headers: http.Header{
				"Connection":            {"Upgrade"},
				"Upgrade":               {"websocket"},
				"Sec-WebSocket-Version": {"13"},
				"Sec-WebSocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
				"Origin":                {"https://game.example"},
				"Cookie":                {"admin_session=session-value"},
			},
		},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, testServer.URL+test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header = test.headers
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
			}
		})
	}
	if got := handled.Load(); got != int32(len(requests)) {
		t.Fatalf("handler calls = %d, want %d", got, len(requests))
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, testServer.URL+"/overcount", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 101 {
		request.Header.Add("X-Count", strconv.Itoa(i))
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("over-limit status = %d, want %d", response.StatusCode, http.StatusRequestHeaderFieldsTooLarge)
	}
	if got := handled.Load(); got != int32(len(requests)) {
		t.Fatalf("over-limit request reached handler: calls = %d", got)
	}
}

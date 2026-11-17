package bootstrap

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestControlHTTPRequestAuthority(t *testing.T) {
	for _, endpoint := range []string{
		"http://127.0.0.1:9090/drain", "http://10.20.30.40:9090/drain",
		"https://control.example.invalid/drain", "http://[fd00::12]:9090/drain",
	} {
		if _, err := controlHTTPRequest(t.Context(), http.MethodPost, endpoint); err != nil {
			t.Errorf("valid operator endpoint %q: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"/drain", "//control.example.invalid/drain", "file:///drain", "http:drain",
		"http://user:private-marker@control.example.invalid/drain", "http://control.example.invalid/drain#fragment",
		"http://control.example.invalid:/drain", "http://control.example.invalid:0/drain", "http://control.example.invalid:65536/drain",
	} {
		_, err := controlHTTPRequest(t.Context(), http.MethodPost, endpoint)
		if err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		} else if strings.Contains(err.Error(), "private-marker") {
			t.Fatal("invalid endpoint credentials appeared in error")
		}
	}
}

func TestControlCommandsDoNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	t.Setenv("CONTROL_TOKEN", "test-control-marker")
	t.Setenv("METRICS_ADDR", strings.TrimPrefix(source.URL, "http://"))
	if err := RunDrainCommand(t.Context(), []string{"--url", source.URL + "/drain"}, io.Discard); err == nil {
		t.Fatal("redirected drain was accepted")
	}
	if err := RunHealthCommand(t.Context(), nil); err == nil {
		t.Fatal("redirected readiness was accepted")
	}
	if count := redirected.Load(); count != 0 {
		t.Fatalf("redirect target received %d requests", count)
	}
}

func TestControlHTTPClientIgnoresProxyEnvironment(t *testing.T) {
	var direct atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-control-marker" {
			t.Error("control token did not reach the configured destination")
		}
		direct.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer target.Close()
	t.Setenv("HTTP_PROXY", "http://proxy.example.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.example.invalid:3128")
	t.Setenv("NO_PROXY", "")
	client := controlHTTPClient(time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("control client permits ambient proxy routing")
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "control.example.invalid:80" {
			t.Errorf("unexpected dial destination %q", address)
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, strings.TrimPrefix(target.URL, "http://"))
	}
	request, err := controlHTTPRequest(t.Context(), http.MethodPost, "http://control.example.invalid/drain")
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-control-marker")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted || direct.Load() != 1 {
		t.Fatalf("status=%d direct requests=%d", response.StatusCode, direct.Load())
	}
}

func TestControlHTTPClientDeadline(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer target.Close()
	request, err := controlHTTPRequest(t.Context(), http.MethodGet, target.URL)
	if err != nil {
		t.Fatal(err)
	}
	response, err := controlHTTPClient(50 * time.Millisecond).Do(request)
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("control request ignored its deadline")
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("request error=%v, expected a timeout", err)
	}
}

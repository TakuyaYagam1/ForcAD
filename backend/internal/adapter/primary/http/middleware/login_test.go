package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type loginBudget struct {
	used map[string]bool
}

func (l *loginBudget) Allow(
	_ context.Context,
	key string,
	_, _, _ int,
) (bool, error) {
	if l.used[key] {
		return false, nil
	}
	l.used[key] = true
	return true, nil
}

func TestLoginLimitAtTrustedIngressDoesNotShareProxyBucket(t *testing.T) {
	budget := &loginBudget{
		used: make(map[string]bool),
	}
	handler := LoginLimit(budget, nil, nil, true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, client := range []string{"192.0.2.1", "198.51.100.2"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login/", nil)
		request.RemoteAddr = "172.19.0.8:40000" // The same ingress for both clients.
		request.Header.Set("X-Forwarded-For", client)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("client %s received %d instead of reaching login", client, response.Code)
		}
	}
}

func TestLoginLimitDirectIgnoresSpoofedForwardedFor(t *testing.T) {
	budget := &loginBudget{
		used: make(map[string]bool),
	}
	handler := LoginLimit(budget, nil, nil, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for index, client := range []string{"192.0.2.1", "198.51.100.2"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login/", nil)
		request.RemoteAddr = "203.0.113.17:40000"
		request.Header.Set("X-Forwarded-For", client)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusNoContent
		if index == 1 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d received %d, want %d", index+1, response.Code, want)
		}
	}
}

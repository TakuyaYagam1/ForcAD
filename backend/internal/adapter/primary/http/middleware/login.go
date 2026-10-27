package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/wahrwelt-kit/go-httpkit/httputil"
)

type RateLimiter interface {
	Allow(ctx context.Context, key string, cost, rate, burst int) (bool, error)
}

func LoginLimit(limiter RateLimiter, trusted []*net.IPNet, onError api.ErrorHandler) func(http.Handler) http.Handler {
	const location = "middleware.LoginLimit"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/admin/login/" && r.Method == http.MethodPost {
				ip := httputil.GetClientIPWithNets(r, trusted)
				ok, err := limiter.Allow(r.Context(), "login:"+ip, 1, 1, 5)
				if err != nil {
					onError(r.Context(), w, r, fmt.Errorf("%s - check login rate: %w", location, err))
					return
				}
				if !ok {
					w.Header().Set("Retry-After", "1")
					WriteError(w, http.StatusTooManyRequests, api.ApiError{Error: "Login rate exceeded"})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

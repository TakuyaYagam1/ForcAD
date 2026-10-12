package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/wahrwelt-kit/go-httpkit/httputil"
)

func Logger(logger *slog.Logger, trusted []*net.IPNet, route func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isHealthRequest(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			response := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func(ctx context.Context) {
				status := response.Status()
				if status == 0 {
					status = http.StatusOK
				}
				level := slog.LevelInfo
				if status >= http.StatusInternalServerError {
					level = slog.LevelError
				} else if status >= http.StatusBadRequest {
					level = slog.LevelWarn
				}
				logger.Log(ctx, level, "http request",
					"request_id", ctx.Value(requestIDKey{}), "method", r.Method,
					"path", route(r), "ip", httputil.GetClientIPWithNets(r, trusted),
					"status", status, "latency_ms", time.Since(start).Milliseconds(), "bytes", response.BytesWritten())
			}(r.Context())
			next.ServeHTTP(response, r)
		})
	}
}

func isHealthRequest(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/flags/health/", "/api/client/health/", "/api/admin/health/", "/api/events/health/":
		return true
	default:
		return false
	}
}

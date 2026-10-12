package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	httpv1 "github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1"
)

func ContextTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if timeout <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ResponseDeadline(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			controller := http.NewResponseController(w)
			_ = controller.SetWriteDeadline(time.Now().Add(timeout))
			defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
			next.ServeHTTP(w, r)
		})
	}
}

func RequestContext(publicOrigin string, trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secure := r.TLS != nil || strings.HasPrefix(publicOrigin, "https://")
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			for _, network := range trusted {
				if network.Contains(net.ParseIP(host)) && r.Header.Get("X-Forwarded-Proto") == "https" {
					secure = true
					break
				}
			}
			session := ""
			if cookie, err := r.Cookie("session"); err == nil {
				session = cookie.Value
			}
			if strings.HasPrefix(r.URL.Path, "/api/admin/") {
				w.Header().Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r.WithContext(httpv1.WithRequestInfo(r.Context(), session, secure)))
		})
	}
}

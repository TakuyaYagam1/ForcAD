package middleware

import (
	"net/http"
	"strings"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(64 << 10)
		if r.URL.Path == "/flags" || r.URL.Path == "/flags/" {
			limit = 32 << 10
		}
		if r.ContentLength > limit {
			WriteError(w, http.StatusRequestEntityTooLarge, api.ApiError{Error: "Request body too large"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func Admission(limit int) func(http.Handler) http.Handler {
	slots := make(chan struct{}, max(1, limit))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/health/") {
				next.ServeHTTP(w, r)
				return
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				w.Header().Set("Retry-After", "1")
				WriteError(w, http.StatusServiceUnavailable, api.ApiError{Error: "Service capacity exceeded"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

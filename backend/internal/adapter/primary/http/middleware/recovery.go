package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/response"
)

func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writer := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func(ctx context.Context) {
				if recovered := recover(); recovered != nil {
					if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(http.ErrAbortHandler)
					}
					logger.ErrorContext(ctx, "panic recovered", "panic", recovered, "stack", string(debug.Stack()))
					if writer.Status() == 0 {
						response.JSON(writer, http.StatusInternalServerError, api.ApiError{Error: "Internal server error"})
					}
				}
			}(r.Context())
			next.ServeHTTP(writer, r)
		})
	}
}

package httpadapter

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/response"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/auth"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/flags"
	"github.com/ogen-go/ogen/ogenerrors"
)

func errorHandler(logger *slog.Logger) api.ErrorHandler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, err error) {
		status, message, mapped := commonRequestError(err)
		if !mapped {
			status, message, mapped = flagRequestError(err, w)
		}
		if !mapped {
			status, message, mapped = requestLimitError(err)
		}
		if !mapped {
			status, message, mapped = ogenRequestError(err, r.URL.Path)
		}
		if !mapped {
			status, message = http.StatusServiceUnavailable, "Service temporarily unavailable"
		}
		if status >= 500 && ctx.Err() == nil {
			logger.Error("request failed", "path", r.URL.Path, "error", err)
		}
		if status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		response.JSON(w, status, api.ApiError{Error: message})
	}
}

func commonRequestError(err error) (int, string, bool) {
	var validation *domain.ValidationError
	var state *domain.StateError
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		return http.StatusForbidden, "Invalid credentials or session", true
	case errors.Is(err, domain.ErrTeamTokenInvalid):
		return http.StatusBadRequest, "Invalid team token", true
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "Not found", true
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, "Data violates database constraints", true
	case errors.As(err, &validation):
		return http.StatusBadRequest, validation.Message, true
	case errors.As(err, &state):
		return http.StatusConflict, state.Message, true
	default:
		return 0, "", false
	}
}

func flagRequestError(err error, w http.ResponseWriter) (int, string, bool) {
	switch {
	case errors.Is(err, flags.ErrNotStarted):
		return http.StatusBadRequest, flags.ErrNotStarted.Error(), true
	case errors.Is(err, flags.ErrInvalidBatch):
		return http.StatusBadRequest, flags.ErrInvalidBatch.Error(), true
	case errors.Is(err, flags.ErrFinished):
		return http.StatusGone, "Game has finished", true
	case errors.Is(err, flags.ErrPaused):
		return http.StatusServiceUnavailable, "Game is paused", true
	case errors.Is(err, flags.ErrLimited):
		w.Header().Set("Retry-After", "1")
		return http.StatusTooManyRequests, "Flag submission rate exceeded", true
	default:
		return 0, "", false
	}
}

func requestLimitError(err error) (int, string, bool) {
	var large *http.MaxBytesError
	switch {
	case errors.As(err, &large):
		return http.StatusRequestEntityTooLarge, "Request body too large", true
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, "Request deadline exceeded", true
	default:
		return 0, "", false
	}
}

func ogenRequestError(err error, path string) (int, string, bool) {
	code := ogenerrors.ErrorCode(err)
	if code == http.StatusUnauthorized && errors.Is(err, ogenerrors.ErrSecurityRequirementIsNotSatisfied) {
		if path == "/flags/" {
			return http.StatusBadRequest, "Invalid team token", true
		}
		return http.StatusForbidden, "Invalid credentials or session", true
	}
	if code >= 400 && code < 500 && code != http.StatusUnauthorized {
		return code, "Invalid request", true
	}
	return 0, "", false
}

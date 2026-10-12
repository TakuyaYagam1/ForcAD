package v1

import (
	"context"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

type healthHandler struct{}

func healthy() (*api.StatusResponse, error) {
	return &api.StatusResponse{Status: api.StatusResponseStatusOk}, nil
}

func (*healthHandler) GetAdminHealth(context.Context) (*api.StatusResponse, error) { return healthy() }

func (*healthHandler) GetClientHealth(context.Context) (*api.StatusResponse, error) { return healthy() }

func (*healthHandler) GetEventsHealth(context.Context) (*api.StatusResponse, error) { return healthy() }

func (*healthHandler) GetFlagReceiverHealth(context.Context) (*api.StatusResponse, error) {
	return healthy()
}

// Metrics and WebSocket upgrades are handled by the HTTP router.

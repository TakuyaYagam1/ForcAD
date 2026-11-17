package v1

import (
	"context"
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type metricsHandler struct{}

func (*metricsHandler) GetAdminMetrics(context.Context) (api.Metrics, error) {
	const location = "httpv1.metricsHandler.GetAdminMetrics"
	return api.Metrics{}, fmt.Errorf("%s - metrics route is not handled here: %w", location, domain.ErrNotFound)
}

func (*metricsHandler) GetClientMetrics(context.Context) (api.Metrics, error) {
	const location = "httpv1.metricsHandler.GetClientMetrics"
	return api.Metrics{}, fmt.Errorf("%s - metrics route is not handled here: %w", location, domain.ErrNotFound)
}

func (*metricsHandler) GetReceiverMetrics(context.Context) (api.Metrics, error) {
	const location = "httpv1.metricsHandler.GetReceiverMetrics"
	return api.Metrics{}, fmt.Errorf("%s - metrics route is not handled here: %w", location, domain.ErrNotFound)
}

package request

import (
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func GameAction(req api.OptGameActionRequest, action string) (*int64, error) {
	const location = "httpv1.request.GameAction"
	data, _ := req.Get()
	if (action == "finish" || action == "start_final") && !data.Confirm.Or(false) {
		return nil, fmt.Errorf("%s - game action confirmation is required: %w", location, &domain.ValidationError{
			Message: "Game action confirmation is required",
		})
	}
	if value, ok := data.Generation.Get(); ok {
		return &value, nil
	}
	return nil, nil
}

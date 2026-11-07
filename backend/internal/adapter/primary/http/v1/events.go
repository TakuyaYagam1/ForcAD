package v1

import (
	"context"
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/response"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type EventService interface {
	List(ctx context.Context, after string) ([]domain.Event, error)
	Announce(
		ctx context.Context,
		title, markdown, author string,
	) (domain.Event, error)
}

func (h *eventsHandler) GetClientEvents(ctx context.Context, params api.GetClientEventsParams) (api.GetClientEventsRes, error) {
	const location = "httpv1.eventsHandler.GetClientEvents"
	events, err := h.events.List(ctx, params.After.Or(""))
	if err != nil {
		return nil, fmt.Errorf("%s - list events: %w", location, err)
	}
	return response.PublicEvents(events), nil
}

func (h *eventsHandler) CreateAdminEvent(ctx context.Context, req *api.AnnouncementCreate) (api.CreateAdminEventRes, error) {
	const location = "httpv1.eventsHandler.CreateAdminEvent"
	author, _ := ctx.Value(usernameKey).(string)
	created, err := h.events.Announce(ctx, req.Title, req.Markdown, author)
	if err != nil {
		return nil, fmt.Errorf("%s - create announcement: %w", location, err)
	}
	event := response.PublicEvent(created)
	return &event, nil
}

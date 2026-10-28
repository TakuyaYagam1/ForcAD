package v1

import (
	"context"
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type EventService interface {
	List(ctx context.Context, after string) ([]domain.Event, error)
	Announce(ctx context.Context, title, markdown, author string) (domain.Event, error)
}

func (h *eventsHandler) GetClientEvents(ctx context.Context, params api.GetClientEventsParams) (api.GetClientEventsRes, error) {
	const location = "httpv1.eventsHandler.GetClientEvents"
	events, err := h.events.List(ctx, params.After.Or(""))
	if err != nil {
		return nil, fmt.Errorf("%s - list events: %w", location, err)
	}
	response := make(api.GetClientEventsOKApplicationJSON, 0, len(events))
	for _, event := range events {
		response = append(response, publicEvent(event))
	}
	return &response, nil
}

func (h *eventsHandler) CreateAdminEvent(ctx context.Context, req *api.AnnouncementCreate) (api.CreateAdminEventRes, error) {
	const location = "httpv1.eventsHandler.CreateAdminEvent"
	author, _ := ctx.Value(usernameKey).(string)
	created, err := h.events.Announce(ctx, req.Title, req.Markdown, author)
	if err != nil {
		return nil, fmt.Errorf("%s - create announcement: %w", location, err)
	}
	event := publicEvent(created)
	return &event, nil
}

func publicEvent(event domain.Event) api.PublicEvent {
	result := api.PublicEvent{
		ID: event.ID, Kind: api.PublicEventKind(event.Kind), CreatedAt: event.CreatedAt,
	}
	if event.AttackerID != nil {
		result.AttackerID = api.NewOptUUID(api.UUID(*event.AttackerID))
	}
	if event.AttackerName != nil {
		result.AttackerName = api.NewOptString(*event.AttackerName)
	}
	if event.VictimID != nil {
		result.VictimID = api.NewOptUUID(api.UUID(*event.VictimID))
	}
	if event.VictimName != nil {
		result.VictimName = api.NewOptString(*event.VictimName)
	}
	if event.TaskID != nil {
		result.TaskID = api.NewOptUUID(api.UUID(*event.TaskID))
	}
	if event.TaskName != nil {
		result.TaskName = api.NewOptString(*event.TaskName)
	}
	if event.AttackerDelta != nil {
		result.AttackerDelta = api.NewOptFloat64(*event.AttackerDelta)
	}
	if event.Round != nil {
		result.Round = api.NewOptInt32(*event.Round)
	}
	if event.Title != nil {
		result.Title = api.NewOptString(*event.Title)
	}
	if event.Markdown != nil {
		result.Markdown = api.NewOptString(*event.Markdown)
	}
	if event.Author != nil {
		result.Author = api.NewOptString(*event.Author)
	}
	return result
}

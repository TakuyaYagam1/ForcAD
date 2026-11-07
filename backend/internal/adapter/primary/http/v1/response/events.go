package response

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func PublicEvent(event domain.Event) api.PublicEvent {
	result := api.PublicEvent{
		ID:        event.ID,
		Kind:      api.PublicEventKind(event.Kind),
		CreatedAt: event.CreatedAt,
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

func PublicEvents(events []domain.Event) *api.GetClientEventsOKApplicationJSON {
	result := make(api.GetClientEventsOKApplicationJSON, 0, len(events))
	for _, event := range events {
		result = append(result, PublicEvent(event))
	}
	return &result
}

package v1

import "github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"

type Handler struct {
	*authHandler
	*gameHandler
	*teamsHandler
	*tasksHandler
	*teamTasksHandler
	*scoreboardHandler
	*flagsHandler
	*healthHandler
	*metricsHandler
	*eventsHandler
}

func NewHandler(game GameService, auth AuthService, flags FlagService, events EventService) *Handler {
	return &Handler{
		authHandler:       &authHandler{auth: auth},
		gameHandler:       &gameHandler{game: game},
		teamsHandler:      &teamsHandler{game: game},
		tasksHandler:      &tasksHandler{game: game},
		teamTasksHandler:  &teamTasksHandler{game: game},
		scoreboardHandler: &scoreboardHandler{game: game},
		flagsHandler:      &flagsHandler{flags: flags},
		healthHandler:     &healthHandler{},
		metricsHandler:    &metricsHandler{},
		eventsHandler:     &eventsHandler{events: events},
	}
}

var _ api.Handler = (*Handler)(nil)
var _ api.RawHandler = (*Handler)(nil)
var _ api.SecurityHandler = (*Handler)(nil)

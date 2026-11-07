//go:build wireinject

package bootstrap

import (
	"context"
	"log/slog"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/prometheus"
	"github.com/goforj/wire"
)

// initialize owns process-scoped resources; Wire composes their cleanup in
// reverse order on either startup failure or normal shutdown.
func initialize(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Application, func(), error) {
	wire.Build(newPostgresPool, postgres.New, postgres.NewGameStore,
		newRedisStore, prometheus.New,
		newBroker, newEvents, newAuth, newGame, newFlags, newEventFeed,
		newHandler, newRouter, newChecker, newScheduler, newTeamTraffic, NewApplication)
	return nil, nil, nil
}

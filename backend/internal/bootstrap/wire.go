//go:build wireinject

package bootstrap

import (
	"context"
	"log/slog"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/prometheus"
	redisadapter "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/redis"
	"github.com/goforj/wire"
)

// initialize owns process-scoped resources; Wire composes their cleanup in
// reverse order on either startup failure or normal shutdown.
func initialize(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Application, func(), error) {
	wire.Build(postgres.NewPool, postgres.New, postgres.NewGameStore,
		redisadapter.New, prometheus.New,
		newBroker, newEvents, newAuth, newGame, newFlags,
		newHandler, newRouter, newChecker, newScheduler, newTeamTraffic, NewApplication)
	return nil, nil, nil
}

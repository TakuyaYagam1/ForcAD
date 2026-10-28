package bootstrap

import (
	"fmt"
	"log/slog"

	promclient "github.com/prometheus/client_golang/prometheus"

	"github.com/TakuyaYagam1/ForcAD/config"
	httpv1 "github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/websocket"
	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/prometheus"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/rabbitmq"
	redisadapter "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/redis"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase"
	checkerworker "github.com/TakuyaYagam1/ForcAD/internal/worker/checker"
	schedulerworker "github.com/TakuyaYagam1/ForcAD/internal/worker/scheduler"
)

func newBroker(cfg config.Config, logger *slog.Logger) *rabbitmq.Broker {
	return rabbitmq.New(cfg.BrokerURL, cfg.CheckerQueue, cfg.WorkerConcurrency, logger)
}
func newEvents(cache *redisadapter.Store, logger *slog.Logger) *websocket.Events {
	return websocket.New(cache.Client, logger)
}
func newAuth(cfg config.Config, cache *redisadapter.Store) *usecase.Auth {
	return usecase.NewAuth(cache, cfg.AdminUsername, cfg.AdminPassword)
}
func newGame(repo *postgres.GameStore, storage *postgres.Repository, events *websocket.Events, logger *slog.Logger) (*usecase.Game, func()) {
	service := usecase.NewGame(repo, storage, events, logger)
	return service, service.Close
}
func newFlags(cfg config.Config, storage *postgres.Repository, game *postgres.GameStore, cache *redisadapter.Store, events *websocket.Events, logger *slog.Logger) *usecase.Flags {
	return usecase.NewFlags(storage, storage, game, cache, events, logger, cfg.FlagRate, cfg.FlagBurst)
}
func newEventFeed(storage *postgres.Repository, events *websocket.Events, logger *slog.Logger) *usecase.Events {
	return usecase.NewEvents(storage, events, logger)
}
func newHandler(game *usecase.Game, auth *usecase.Auth, flags *usecase.Flags, feed *usecase.Events) *httpv1.Handler {
	return httpv1.NewHandler(game, auth, flags, feed)
}
func newRouter(cfg config.Config, handler *httpv1.Handler, events *websocket.Events, metrics *prometheus.Metrics, cache *redisadapter.Store, logger *slog.Logger, traffic *prometheus.TeamTraffic) (apiHandler, error) {
	const location = "bootstrap.newRouter"
	metrics.Registry.MustRegister(
		promclient.NewGaugeFunc(promclient.GaugeOpts{Name: "forcad_websocket_connections", Help: "WebSocket slots in use, including upgrades."}, func() float64 { return float64(events.Connections()) }),
		promclient.NewGaugeFunc(promclient.GaugeOpts{Name: "forcad_websocket_connection_limit", Help: "WebSocket slot limit per process."}, func() float64 { return float64(events.ConnectionLimit()) }),
		promclient.NewCounterFunc(promclient.CounterOpts{Name: "forcad_websocket_slow_client_drops_total", Help: "Slow subscriber deliveries dropped by the event hub."}, func() float64 { return float64(events.SlowClientDrops()) }),
		promclient.NewCounterFunc(promclient.CounterOpts{Name: "forcad_websocket_notification_drops_total", Help: "Optional activity notifications skipped for full subscriber queues."}, func() float64 { return float64(events.NotificationDrops()) }),
	)
	router, err := httpv1.NewRouter(cfg, handler, events, metrics.Export, metrics.Registry, cache, logger, traffic)
	if err != nil {
		return apiHandler{}, fmt.Errorf("%s - create HTTP router: %w", location, err)
	}
	return apiHandler{router}, nil
}
func newChecker(cfg config.Config, repo *postgres.Repository, game *postgres.GameStore, logger *slog.Logger) *checkerworker.Worker {
	runner := processchecker.NewRunner(processchecker.Options{MemoryLimitMB: cfg.CheckerMemoryMB, DisableMemoryLimit: cfg.CheckerMemoryMB == 0, MaxOutputBytes: cfg.CheckerOutputBytes})
	return checkerworker.New(repo, repo, game, runner, checkerworker.Options{WorkConcurrency: cfg.WorkerConcurrency, LeaseDuration: cfg.JobGrace, RecoveryGrace: cfg.JobGrace, MaxWorkflowDuration: cfg.CheckerWorkflowTimeout}, logger)
}

func newScheduler(cfg config.Config, repo *postgres.Repository, game *postgres.GameStore, broker *rabbitmq.Broker, logger *slog.Logger) scheduledWorker {
	return schedulerworker.New(repo, game, broker, logger, schedulerworker.Options{RecoveryGrace: cfg.JobGrace})
}

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	promclient "github.com/prometheus/client_golang/prometheus"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/websocket"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/prometheus"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/rabbitmq"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/flags"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase/game"
	checkerworker "github.com/TakuyaYagam1/ForcAD/internal/worker/checker"
)

type apiHandler struct{ http.Handler }
type scheduledWorker interface {
	Run(ctx context.Context) error
}

type Application struct {
	config    config.Config
	handler   apiHandler
	metrics   *prometheus.Metrics
	logger    *slog.Logger
	events    *websocket.Events
	game      *game.Service
	flags     *flags.Service
	broker    *rabbitmq.Broker
	checker   *checkerworker.Worker
	scheduler scheduledWorker
	traffic   *prometheus.TeamTraffic
}

func NewApplication(cfg config.Config, handler apiHandler, metrics *prometheus.Metrics, logger *slog.Logger, events *websocket.Events, game *game.Service, flags *flags.Service, broker *rabbitmq.Broker, checker *checkerworker.Worker, scheduler scheduledWorker, traffic *prometheus.TeamTraffic) *Application {
	return &Application{config: cfg, handler: handler, metrics: metrics, logger: logger, events: events, game: game, flags: flags, broker: broker, checker: checker, scheduler: scheduler, traffic: traffic}
}

func RunApplication(cfg config.Config, logger *Logger) error {
	const location = "bootstrap.RunApplication"
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, cleanup, err := initialize(ctx, cfg, logger.Logger)
	if err != nil {
		return fmt.Errorf("%s - initialize application: %w", location, err)
	}
	defer cleanup()
	app.metrics.Registry.MustRegister(promclient.NewCounterFunc(promclient.CounterOpts{
		Name: "forcad_log_dropped_total", Help: "Log entries dropped because the bounded output queue was full.",
	}, func() float64 { return float64(logger.dropped.Load()) }))
	return app.Run(ctx)
}

func (a *Application) Run(ctx context.Context) error {
	const location = "bootstrap.Application.Run"
	defer a.game.Close()
	if _, err := a.game.Lifecycle(ctx); err != nil {
		return fmt.Errorf("%s - read initialized game: %w", location, err)
	}
	if err := a.traffic.Refresh(ctx); err != nil {
		a.logger.Warn("initial team metrics inventory unavailable", "error", err)
	}
	servers, err := a.listenServers(ctx)
	if err != nil {
		return err
	}
	defer closeListeners(servers)
	workCtx, forceStop := context.WithCancel(context.WithoutCancel(ctx))
	defer forceStop()
	stopSharedReads := context.AfterFunc(workCtx, a.game.Close)
	defer stopSharedReads()
	schedulerCtx, stopScheduler := context.WithCancel(workCtx)
	defer stopScheduler()
	backgroundCtx, stopBackground := context.WithCancel(workCtx)
	defer stopBackground()
	failures := make(chan error, len(servers)+2)
	httpServers := serveHTTP(servers, failures)
	background := a.startBackground(backgroundCtx)
	jobs := a.startJobs(workCtx, schedulerCtx, failures)
	a.logger.Info("application ready", "role", a.config.Role, "http_addr", a.config.HTTPAddr, "metrics_addr", a.config.MetricsAddr)
	var runErr error
	select {
	case <-ctx.Done():
	case <-a.metrics.DrainRequested():
	case runErr = <-failures:
	}
	a.logger.Info("draining application", "role", a.config.Role)
	a.metrics.Drain()
	a.broker.Drain()
	stopScheduler()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.config.ShutdownTimeout)
	defer cancel()
	force := context.AfterFunc(shutdownCtx, forceStop)
	defer force()
	shutdown := shutdownHTTP(shutdownCtx, servers)
	jobs.Wait()
	shutdown.Wait()
	stopBackground()
	background.Wait()
	httpServers.Wait()
	if shutdownCtx.Err() != nil {
		runErr = errors.Join(runErr, fmt.Errorf("%s - shutdown deadline exceeded; unfinished work will be recovered: %w", location, shutdownCtx.Err()))
	}
	a.logger.Info("application stopped", "role", a.config.Role)
	return runErr
}

// boundHTTP keeps each server with the listener it owns.
type boundHTTP struct {
	server   *http.Server
	listener net.Listener
}

func (a *Application) listenServers(ctx context.Context) ([]boundHTTP, error) {
	const location = "bootstrap.Application.listenServers"
	control, err := listenHTTP(ctx, a.config.MetricsAddr, a.metrics.Control)
	if err != nil {
		return nil, fmt.Errorf("%s - listen on METRICS_ADDR: %w", location, err)
	}
	servers := []boundHTTP{control}
	if a.config.Role == "api" || a.config.Role == "receiver" || a.config.Role == "all" {
		public, err := listenHTTP(ctx, a.config.HTTPAddr, a.metrics.Admission(a.handler))
		if err != nil {
			closeListeners(servers)
			return nil, fmt.Errorf("%s - listen on HTTP_ADDR: %w", location, err)
		}
		servers = append(servers, public)
	}
	return servers, nil
}

func listenHTTP(ctx context.Context, address string, handler http.Handler) (boundHTTP, error) {
	const location = "bootstrap.listenHTTP"
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return boundHTTP{}, fmt.Errorf("%s - bind listener: %w", location, err)
	}
	return boundHTTP{server: newHTTPServer(handler), listener: listener}, nil
}

func closeListeners(servers []boundHTTP) {
	for _, server := range servers {
		_ = server.listener.Close()
	}
}

func serveHTTP(servers []boundHTTP, failures chan<- error) *sync.WaitGroup {
	const location = "bootstrap.serveHTTP"
	var running sync.WaitGroup
	for _, server := range servers {
		running.Go(func() {
			if err := server.server.Serve(server.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				failures <- fmt.Errorf("%s - serve HTTP listener: %w", location, err)
			}
		})
	}
	return &running
}

func shutdownHTTP(ctx context.Context, servers []boundHTTP) *sync.WaitGroup {
	var shutdown sync.WaitGroup
	for _, server := range servers {
		shutdown.Go(func() {
			if err := server.server.Shutdown(ctx); err != nil {
				_ = server.server.Close()
			}
		})
	}
	return &shutdown
}

func (a *Application) startBackground(ctx context.Context) *sync.WaitGroup {
	var background sync.WaitGroup
	background.Go(func() { a.traffic.Run(ctx) })
	if a.config.Role == "api" || a.config.Role == "all" {
		background.Go(func() { a.events.Run(ctx) })
		background.Go(func() { a.game.Run(ctx) })
	}
	if a.config.Role == "receiver" || a.config.Role == "all" {
		background.Go(func() { a.flags.Run(ctx) })
	}
	return &background
}

func (a *Application) startJobs(ctx, schedulerCtx context.Context, failures chan<- error) *sync.WaitGroup {
	var jobs sync.WaitGroup
	if a.config.Role == "worker" || a.config.Role == "all" {
		jobs.Go(func() {
			if err := a.broker.Run(ctx, a.checker.Handle); err != nil {
				failures <- err
			}
		})
	}
	if a.config.Role == "ticker" || a.config.Role == "all" {
		jobs.Go(func() {
			if err := a.scheduler.Run(schedulerCtx); err != nil && schedulerCtx.Err() == nil {
				failures <- err
			}
		})
	}
	return &jobs
}

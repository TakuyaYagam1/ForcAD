package httpadapter

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/wahrwelt-kit/go-httpkit/httputil"
	"github.com/wahrwelt-kit/go-httpkit/metrics"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/middleware"
	httpv1 "github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1"
)

type Traffic interface {
	Middleware(route func(*http.Request) string) func(http.Handler) http.Handler
}

func NewRouter(cfg config.Config, handler *httpv1.Handler, events http.Handler, monitor http.Handler, registry prometheus.Registerer, limiter middleware.RateLimiter, logger *slog.Logger, traffic ...Traffic) (http.Handler, error) {
	const location = "httpadapter.NewRouter"
	server, err := api.NewServer(handler, handler, handler, api.WithErrorHandler(errorHandler(logger)))
	if err != nil {
		return nil, fmt.Errorf("%s - create API server: %w", location, err)
	}
	routeName := func(req *http.Request) string {
		path := req.URL.Path
		if path == "/flags" {
			path = "/flags/"
		}
		if path == "/api/events/ws" {
			return "/api/events/ws"
		}
		if route, ok := server.FindRoute(req.Method, path); ok {
			return route.PathPattern()
		}
		return "unmatched"
	}
	trusted, err := httputil.ParseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, fmt.Errorf("%s - parse trusted proxy networks: %w", location, err)
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(logger, trusted, routeName))
	for _, observer := range traffic {
		if observer != nil {
			r.Use(observer.Middleware(routeName))
		}
	}
	r.Use(middleware.Recoverer(logger), middleware.SecurityHeaders)
	r.Use(metrics.Middleware(registry, routeName, metrics.WithNamespace("forcad"), metrics.WithInFlight(), metrics.WithLogger(logger)))
	registerMetricsRoutes(r, cfg.Role, events, monitor)
	r.Group(func(r chi.Router) {
		r.Use(middleware.ResponseDeadline(cfg.RequestTimeout + 5*time.Second))
		r.Use(middleware.ContextTimeout(cfg.RequestTimeout))
		r.Use(middleware.RequestContext(cfg.PublicOrigin, trusted))
		r.Use(middleware.AdminOrigin(cfg.PublicOrigin))
		r.Use(middleware.LoginLimit(limiter, trusted, errorHandler(logger)))
		r.Use(middleware.BodyLimit)
		r.Use(middleware.Admission(cfg.HTTPMaxInflight))
		r.Handle("/*", requestDispatcher(server, cfg.Role))
	})
	return r, nil
}

func registerMetricsRoutes(router chi.Router, role string, events, monitor http.Handler) {
	if role == "api" || role == "all" {
		router.Get("/api/events/ws", events.ServeHTTP)
		for _, path := range []string{"/api/admin/metrics", "/api/client/metrics"} {
			router.Get(path, monitor.ServeHTTP)
		}
	}
	if role == "receiver" || role == "all" {
		router.Get("/api/http-receiver/metrics", monitor.ServeHTTP)
	}
}

func requestDispatcher(server http.Handler, role string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path
		if !roleAllowsPath(role, path) {
			http.NotFound(w, req)
			return
		}
		if path == "/flags" {
			clone := req.Clone(req.Context())
			clone.URL.Path = "/flags/"
			req = clone
		}
		server.ServeHTTP(w, req)
	})
}

func roleAllowsPath(role, path string) bool {
	admin := strings.HasPrefix(path, "/api/admin/")
	client := strings.HasPrefix(path, "/api/client/") || path == "/api/events/health/"
	receiver := path == "/flags" || strings.HasPrefix(path, "/flags/")
	return role == "all" || role == "api" && (admin || client) || role == "receiver" && receiver
}

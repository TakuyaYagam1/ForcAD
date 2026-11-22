package v1

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
	"github.com/TakuyaYagam1/ForcAD/internal/backup"
)

const backupTimeout = 10 * time.Minute

type Traffic interface {
	Middleware(route func(*http.Request) string) func(http.Handler) http.Handler
}

func NewRouter(
	cfg config.Config,
	handler *Handler,
	events http.Handler,
	monitor http.Handler,
	registry prometheus.Registerer,
	limiter middleware.RateLimiter,
	logger *slog.Logger,
	traffic ...Traffic,
) (http.Handler, error) {
	const location = "httpv1.NewRouter"
	server, err := api.NewServer(handler, handler, handler, api.WithErrorHandler(errorHandler(logger)))
	if err != nil {
		return nil, fmt.Errorf("%s - create API server: %w", location, err)
	}
	routeName := func(req *http.Request) string {
		path := req.URL.Path
		if path == "/flags" {
			path = "/flags/"
		}
		if path == "/api/v1/events/ws" {
			return "/api/v1/events/ws"
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
	serveLogo := http.NotFound
	if handler.BackupHandler != nil && (cfg.Role == "api" || cfg.Role == "all") {
		r.Group(func(r chi.Router) {
			// Backup transfers use separate time and body limits from JSON requests.
			r.Use(middleware.ResponseDeadline(backupTimeout + 5*time.Second))
			r.Use(middleware.ContextTimeout(backupTimeout))
			r.Use(middleware.RequestContext(cfg.PublicOrigin, trusted))
			r.Use(middleware.AdminOrigin(cfg.PublicOrigin))
			r.Use(middleware.Admission(cfg.HTTPMaxInflight))
			r.Handle("/api/v1/admin/backup/*", backupTransfer(server))
		})
		serveLogo = server.ServeHTTP
	} else {
		r.Handle("/api/v1/admin/backup/*", http.NotFoundHandler())
	}
	r.With(middleware.ResponseDeadline(cfg.RequestTimeout+5*time.Second),
		middleware.ContextTimeout(cfg.RequestTimeout), middleware.Admission(cfg.HTTPMaxInflight)).
		Get("/api/v1/client/team-logos/{name}", serveLogo)
	r.Group(func(r chi.Router) {
		r.Use(middleware.ResponseDeadline(cfg.RequestTimeout + 5*time.Second))
		r.Use(middleware.ContextTimeout(cfg.RequestTimeout))
		r.Use(middleware.RequestContext(cfg.PublicOrigin, trusted))
		r.Use(middleware.AdminOrigin(cfg.PublicOrigin))
		r.Use(middleware.LoginLimit(limiter, trusted, errorHandler(logger), cfg.LoginRateLimitAtProxy))
		r.Use(middleware.BodyLimit)
		r.Use(middleware.Admission(cfg.HTTPMaxInflight))
		r.Handle("/*", requestDispatcher(server, cfg.Role))
	})
	return r, nil
}

func backupTransfer(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.ContentLength > backup.MaxUploadBytes {
				middleware.WriteError(w, http.StatusRequestEntityTooLarge, api.ApiError{
					Error: "Backup archive is too large",
				})
				return
			}
			if r.Header.Get("Content-Type") != "application/gzip" {
				middleware.WriteError(w, http.StatusUnsupportedMediaType, api.ApiError{
					Error: "Expected an application/gzip archive",
				})
				return
			}
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(time.Now().Add(backupTimeout))
			defer func() {
				_ = controller.SetReadDeadline(time.Time{})
			}()
			r.Body = http.MaxBytesReader(w, r.Body, backup.MaxUploadBytes)
		}
		next.ServeHTTP(w, r)
	}
}

func registerMetricsRoutes(
	router chi.Router,
	role string,
	events, monitor http.Handler,
) {
	if role == "api" || role == "all" {
		router.Get("/api/v1/events/ws", events.ServeHTTP)
		for _, path := range []string{"/api/v1/admin/metrics", "/api/v1/client/metrics"} {
			router.Get(path, monitor.ServeHTTP)
		}
	}
	if role == "receiver" || role == "all" {
		router.Get("/api/v1/http-receiver/metrics", monitor.ServeHTTP)
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
	admin := strings.HasPrefix(path, "/api/v1/admin/")
	client := strings.HasPrefix(path, "/api/v1/client/") || path == "/api/v1/events/health/"
	receiver := path == "/flags" || strings.HasPrefix(path, "/flags/")
	return role == "all" || role == "api" && (admin || client) || role == "receiver" && receiver
}

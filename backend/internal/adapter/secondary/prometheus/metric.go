package prometheus

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wahrwelt-kit/go-httpkit/httputil"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/rabbitmq"
	redisadapter "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/redis"
)

type checkFunc func(context.Context) error

func (f checkFunc) Check(ctx context.Context) error { return f(ctx) }

type Metrics struct {
	Registry  *prometheus.Registry
	Export    http.Handler
	Control   http.Handler
	draining  atomic.Bool
	drain     chan struct{}
	drainOnce sync.Once
}

func New(cfg config.Config, pool *pgxpool.Pool, cache *redisadapter.Store, broker *rabbitmq.Broker) *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry(), drain: make(chan struct{})}
	m.Registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	for name, sample := range map[string]func() float64{
		"forcad_database_connections":        func() float64 { return float64(pool.Stat().TotalConns()) },
		"forcad_database_connections_in_use": func() float64 { return float64(pool.Stat().AcquiredConns()) },
		"forcad_database_connection_limit":   func() float64 { return float64(pool.Stat().MaxConns()) },
		"forcad_redis_connections":           func() float64 { return float64(cache.Client.PoolStats().TotalConns) },
		"forcad_redis_connections_idle":      func() float64 { return float64(cache.Client.PoolStats().IdleConns) },
		"forcad_redis_pool_pending_requests": func() float64 { return float64(cache.Client.PoolStats().PendingRequests) },
		"forcad_worker_action_limit":         func() float64 { return float64(cfg.WorkerConcurrency) },
		"forcad_draining": func() float64 {
			if m.draining.Load() {
				return 1
			}
			return 0
		},
		"forcad_broker_connected": func() float64 {
			if broker.Connected() {
				return 1
			}
			return 0
		},
	} {
		m.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: strings.ReplaceAll(name, "_", " ")}, sample))
	}
	m.Registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "forcad_database_acquire_canceled_total", Help: "Canceled database pool acquisitions."}, func() float64 { return float64(pool.Stat().CanceledAcquireCount()) }))
	for name, sample := range map[string]func() float64{
		"forcad_database_acquire_wait_seconds_total": func() float64 { return pool.Stat().EmptyAcquireWaitTime().Seconds() },
		"forcad_database_acquire_wait_total":         func() float64 { return float64(pool.Stat().EmptyAcquireCount()) },
		"forcad_redis_pool_timeouts_total":           func() float64 { return float64(cache.Client.PoolStats().Timeouts) },
		"forcad_redis_pool_wait_total":               func() float64 { return float64(cache.Client.PoolStats().WaitCount) },
		"forcad_redis_pool_wait_seconds_total":       func() float64 { return float64(cache.Client.PoolStats().WaitDurationNs) / float64(time.Second) },
	} {
		m.Registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: strings.ReplaceAll(name, "_", " ")}, sample))
	}
	m.Export = promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Timeout: 3 * time.Second, MaxRequestsInFlight: 4})
	ready := httputil.HealthHandler(map[string]httputil.Checker{
		"postgres": checkFunc(func(ctx context.Context) error {
			const location = "prometheus.New"
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("%s - ping PostgreSQL: %w", location, err)
			}
			return nil
		}),
		"redis": checkFunc(func(ctx context.Context) error {
			const location = "prometheus.New"
			if err := cache.Client.Ping(ctx).Err(); err != nil {
				return fmt.Errorf("%s - ping Redis: %w", location, err)
			}
			return nil
		}),
	}, httputil.HealthTimeout(2*time.Second), httputil.HealthHideDetails())
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Export)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httputil.RenderJSON(w, r, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if m.draining.Load() || (cfg.Role == "worker" || cfg.Role == "all") && !broker.Connected() {
			httputil.RenderJSON(w, r, 503, map[string]string{"status": "unavailable"})
			return
		}
		ready(w, r)
	})
	mux.HandleFunc("POST /drain", func(w http.ResponseWriter, r *http.Request) {
		expected, actual := sha256.Sum256([]byte(cfg.ControlToken)), sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
		if cfg.ControlToken == "" || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		m.Drain()
		httputil.RenderJSON(w, r, 202, map[string]string{"status": "draining"})
	})
	m.Control = mux
	return m
}

func (m *Metrics) Drain()                          { m.draining.Store(true); m.drainOnce.Do(func() { close(m.drain) }) }
func (m *Metrics) DrainRequested() <-chan struct{} { return m.drain }
func (m *Metrics) Admission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.draining.Load() {
			w.Header().Set("Retry-After", "1")
			httputil.RenderJSON(w, r, 503, map[string]string{"error": "Service is draining"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

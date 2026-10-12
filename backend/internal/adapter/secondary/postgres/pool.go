package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/config"
)

func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, func(), error) {
	const location = "postgres.NewPool"

	settings, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("%s - parse PostgreSQL connection settings: invalid PostgreSQL connection settings", location)
	}
	settings.MaxConns = cfg.DatabaseMaxConns
	settings.MinConns = 0
	settings.MaxConnLifetime = 30 * time.Minute
	settings.MaxConnLifetimeJitter = 5 * time.Minute
	settings.MaxConnIdleTime = 5 * time.Minute
	settings.HealthCheckPeriod = 30 * time.Second
	settings.ConnConfig.ConnectTimeout = cfg.DatabaseTimeout
	settings.ConnConfig.RuntimeParams["application_name"] = "forcad-" + cfg.Role
	if cfg.DatabasePoolerAddr == "" {
		settings.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.DatabaseTimeout.Milliseconds(), 10)
		settings.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
		settings.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"
	}
	// PgBouncer sets server timeouts in connect_query. Session startup SETs
	// cannot follow a client between transaction-pooled server connections.
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		return nil, nil, fmt.Errorf("%s - create PostgreSQL connection pool: %w", location, err)
	}
	probe, cancel := context.WithTimeout(ctx, cfg.DatabaseTimeout)
	defer cancel()
	if err := pool.Ping(probe); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("%s - verify PostgreSQL connection: %w", location, err)
	}
	return pool, pool.Close, nil
}

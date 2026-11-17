package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolOptions struct {
	URL                string
	MaxConns           int32
	MinConns           int32
	Timeout            time.Duration
	ApplicationName    string
	TransactionPooling bool
}

// NewPool owns one process-scoped pool. Repositories borrowing it do not close it.
func NewPool(ctx context.Context, options PoolOptions) (*pgxpool.Pool, func(), error) {
	const location = "postgres.NewPool"

	settings, err := poolSettings(options)
	if err != nil {
		return nil, nil, fmt.Errorf("%s - parse PostgreSQL connection settings: invalid PostgreSQL connection settings", location)
	}
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		return nil, nil, fmt.Errorf("%s - create PostgreSQL connection pool: %w", location, err)
	}
	probe, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	if err := pool.Ping(probe); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("%s - verify PostgreSQL connection: %w", location, err)
	}
	return pool, pool.Close, nil
}

func poolSettings(options PoolOptions) (*pgxpool.Config, error) {
	const location = "postgres.poolSettings"

	settings, err := pgxpool.ParseConfig(options.URL)
	if err != nil {
		return nil, fmt.Errorf("%s - parse PostgreSQL connection settings: %w", location, err)
	}
	settings.MaxConns = options.MaxConns
	settings.MinConns = options.MinConns
	settings.MaxConnLifetime = 30 * time.Minute
	settings.MaxConnLifetimeJitter = 5 * time.Minute
	settings.MaxConnIdleTime = 5 * time.Minute
	settings.HealthCheckPeriod = 30 * time.Second
	settings.ConnConfig.ConnectTimeout = options.Timeout
	settings.ConnConfig.RuntimeParams["application_name"] = options.ApplicationName
	if !options.TransactionPooling {
		settings.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(options.Timeout.Milliseconds(), 10)
		settings.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
		settings.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"
	}
	// PgBouncer sets server timeouts in connect_query. Session startup SETs
	// cannot follow a client between transaction-pooled server connections.
	return settings, nil
}

package bootstrap

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	redisadapter "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/redis"
)

// Storage factories own process-scoped resources. Repositories, cache users and
// metrics receive the same instances, while Wire composes their cleanup once.
func newPostgresPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, func(), error) {
	const location = "bootstrap.newPostgresPool"

	pool, cleanup, err := postgres.NewPool(ctx, postgresPoolOptions(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("%s - initialize PostgreSQL connection pool: %w", location, err)
	}
	return pool, cleanup, nil
}

func postgresPoolOptions(cfg config.Config) postgres.PoolOptions {
	name := "forcad-" + cfg.Role
	if instance, err := os.Hostname(); err == nil && instance != "" {
		name += "/" + instance
	}
	// PostgreSQL truncates application_name at 63 bytes.
	if len(name) > 63 {
		name = name[:63]
	}
	return postgres.PoolOptions{
		URL:                cfg.DatabaseURL,
		MaxConns:           cfg.DatabaseMaxConns,
		MinConns:           cfg.DatabaseMinConns,
		Timeout:            cfg.DatabaseTimeout,
		ApplicationName:    name,
		TransactionPooling: cfg.DatabasePoolerAddr != "",
	}
}

func newRedisStore(ctx context.Context, cfg config.Config) (*redisadapter.Store, func(), error) {
	const location = "bootstrap.newRedisStore"

	store, cleanup, err := redisadapter.New(ctx, redisadapter.ConnectionOptions{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%s - initialize Redis connection: %w", location, err)
	}
	return store, cleanup, nil
}

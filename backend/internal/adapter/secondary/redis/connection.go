package redisadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	cachekit "github.com/wahrwelt-kit/go-cachekit"
)

type ConnectionOptions struct {
	Addr     string
	Password string
	DB       int
}

// New owns one process-scoped client shared by the cache, limiter and pub/sub.
func New(ctx context.Context, options ConnectionOptions) (*Store, func(), error) {
	const location = "redisadapter.New"
	client := redis.NewClient(connectionSettings(options))
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(probe).Err(); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("%s - connect to Redis: %w", location, err)
	}
	return &Store{
		Client: client,
		RedisKeyValueStore: cachekit.RedisKeyValueStore{
			Client: client,
		},
	}, func() {
		_ = client.Close()
	}, nil
}

func connectionSettings(options ConnectionOptions) *redis.Options {
	return &redis.Options{
		Addr:                  options.Addr,
		Password:              options.Password,
		DB:                    options.DB,
		PoolSize:              16,
		MaxActiveConns:        32,
		PoolTimeout:           time.Second,
		DialTimeout:           2 * time.Second,
		ReadTimeout:           2 * time.Second,
		WriteTimeout:          2 * time.Second,
		ContextTimeoutEnabled: true,
		MaxRetries:            1,
	}
}

package redisadapter

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	cachekit "github.com/wahrwelt-kit/go-cachekit"
)

type Store struct {
	cachekit.RedisKeyValueStore

	Client *redis.Client
}

// FlushDB clears only the configured logical database. Operator reset invokes
// this after writers have stopped; never use FLUSHALL on a shared Redis server.
func (s *Store) FlushDB(ctx context.Context) error {
	const location = "redisadapter.Store.FlushDB"
	if err := s.Client.FlushDB(ctx).Err(); err != nil {
		return fmt.Errorf("%s - flush configured Redis database: %w", location, err)
	}
	return nil
}

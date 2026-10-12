package redisadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	cachekit "github.com/wahrwelt-kit/go-cachekit"

	"github.com/TakuyaYagam1/ForcAD/config"
)

type Store struct {
	cachekit.RedisKeyValueStore

	Client *redis.Client
}

func New(ctx context.Context, cfg config.Config) (*Store, func(), error) {
	const location = "redisadapter.New"
	client := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB,
		PoolSize: 16, MaxActiveConns: 32, PoolTimeout: time.Second,
		DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second,
		ContextTimeoutEnabled: true, MaxRetries: 1,
	})
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(probe).Err(); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("%s - connect to Redis: %w", location, err)
	}
	return &Store{Client: client, RedisKeyValueStore: cachekit.RedisKeyValueStore{Client: client}}, func() { _ = client.Close() }, nil
}

// Redis time keeps a team's shared bucket consistent across receiver replicas.
var consume = redis.NewScript(`
local now = redis.call('TIME')
local current = tonumber(now[1]) + tonumber(now[2]) / 1000000
local prior = redis.call('HMGET', KEYS[1], 'tokens', 'time')
local rate, burst, cost = tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3])
local tokens = tonumber(prior[1]) or burst
local previous = tonumber(prior[2]) or current
tokens = math.min(burst, tokens + math.max(0, current - previous) * rate)
local allowed = 0
if tokens >= cost then tokens = tokens - cost; allowed = 1 end
redis.call('HSET', KEYS[1], 'tokens', tokens, 'time', current)
redis.call('EXPIRE', KEYS[1], math.max(2, math.ceil(burst / rate) * 2))
return allowed
`)

// Allow charges work units, not requests: one batch of 100 flags costs 100.
func (s *Store) Allow(ctx context.Context, scope string, cost, rate, burst int) (bool, error) {
	const location = "redisadapter.Store.Allow"
	if cost < 0 || rate <= 0 || burst <= 0 {
		return false, fmt.Errorf("%s - invalid rate limit configuration", location)
	}
	if cost == 0 {
		return true, nil
	}
	n, err := consume.Run(ctx, s.Client, []string{"forcad:limit:" + scope}, rate, burst, cost).Int()
	if err != nil {
		return false, fmt.Errorf("%s - consume rate limit tokens: %w", location, err)
	}
	return n == 1, nil
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

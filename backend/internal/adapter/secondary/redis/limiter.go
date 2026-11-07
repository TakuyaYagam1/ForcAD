package redisadapter

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

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
func (s *Store) Allow(
	ctx context.Context,
	scope string,
	cost, rate, burst int,
) (bool, error) {
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

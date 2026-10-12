package redis_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	redisadapter "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/redis"
)

func TestSharedBucketChargesFlagsAcrossReceivers(t *testing.T) {
	addr := os.Getenv("FORCAD_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("FORCAD_TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, closeFirst, err := redisadapter.New(ctx, config.Config{RedisAddr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFirst()
	second, closeSecond, err := redisadapter.New(ctx, config.Config{RedisAddr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer closeSecond()
	scope := "test:" + t.Name() + ":" + time.Now().Format("150405.000000000")
	defer first.Client.Del(context.Background(), "forcad:limit:"+scope)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := range 10 {
		store := first
		if i%2 == 1 {
			store = second
		}
		wg.Go(func() {
			ok, err := store.Allow(ctx, scope, 100, 1, 200)
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("accepted %d batches, expected shared budget of 200 flags", accepted.Load())
	}
}

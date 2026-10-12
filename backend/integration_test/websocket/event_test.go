package websocket_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/websocket"
	"github.com/redis/go-redis/v9"
)

func TestRedisSubscriberStopsOnCancellation(t *testing.T) {
	addr := os.Getenv("FORCAD_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("FORCAD_TEST_REDIS_ADDR is not set")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := websocket.New(client, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() { defer close(done); events.Run(ctx) }()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		counts, err := client.PubSubNumSub(ctx, "forcad:events").Result()
		if err != nil {
			t.Fatal(err)
		}
		if counts["forcad:events"] > 0 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("Redis subscription did not start")
		case <-tick.C:
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Redis subscription ignored cancellation")
	}
}

package nats_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"
	"uuid"

	natsbroker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/nats"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type fixture struct {
	config natsbroker.Config
	broker *natsbroker.Broker
	conn   *nats.Conn
	js     jetstream.JetStream
}

func newFixture(tb testing.TB, concurrency int) fixture {
	tb.Helper()
	address := os.Getenv("FORCAD_TEST_NATS_URL")
	if address == "" {
		tb.Skip("FORCAD_TEST_NATS_URL is not set")
	}
	prefix := os.Getenv("FORCAD_TEST_NATS_PREFIX")
	if !regexp.MustCompile(`^forcad_(test|e2e)_[A-Za-z0-9_]+$`).MatchString(prefix) {
		tb.Fatal("integration NATS namespace must be an explicit forcad_test_ or forcad_e2e_ prefix")
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	config := natsbroker.Config{
		URL:           address,
		Stream:        prefix + "_" + suffix,
		Subject:       prefix + "." + suffix + ".jobs",
		Consumer:      "workers",
		Concurrency:   concurrency,
		MaxAckPending: 64,
	}
	conn, err := nats.Connect(address, nats.Timeout(3*time.Second))
	if err != nil {
		tb.Fatal(err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		tb.Fatal(err)
	}
	broker := natsbroker.New(config, slog.New(slog.DiscardHandler))
	f := fixture{
		config: config,
		broker: broker,
		conn:   conn,
		js:     js,
	}
	tb.Cleanup(func() {
		_ = broker.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = js.DeleteStream(ctx, config.Stream)
		conn.Close()
	})
	ctx, cancel := context.WithTimeout(tb.Context(), 5*time.Second)
	defer cancel()
	if err := broker.Ensure(ctx); err != nil {
		tb.Fatal(err)
	}
	return f
}

type running struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func runBroker(t *testing.T, b *natsbroker.Broker, handle natsbroker.Handler) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	run := &running{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		defer close(run.done)
		run.err = b.Run(ctx, handle)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-run.done:
			if run.err != nil {
				t.Errorf("broker run: %v", run.err)
			}
		case <-time.After(7 * time.Second):
			t.Error("broker did not stop")
		}
	})
	return run
}

func await(t *testing.T, predicate func() bool) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if predicate() {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for NATS state")
		case <-ticker.C:
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
}

func publish(t *testing.T, b *natsbroker.Broker, ids ...uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	confirmed, err := b.Publish(ctx, ids)
	if err != nil || !slices.Equal(ids, confirmed) {
		t.Fatalf("confirmed=%v want=%v err=%v", confirmed, ids, err)
	}
}

func job(index int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", index))
}
func empty(t *testing.T, f fixture) {
	t.Helper()
	await(t, func() bool {
		info, err := f.js.Stream(t.Context(), f.config.Stream)
		return err == nil && info.CachedInfo().State.Msgs == 0
	})
}

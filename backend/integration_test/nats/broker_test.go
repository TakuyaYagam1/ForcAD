package nats_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	natsbroker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/nats"
	"github.com/nats-io/nats.go/jetstream"
)

func TestDrainFinishesCurrentDeliveryAndLeavesQueuedWork(t *testing.T) {
	f := newFixture(t, 1)
	entered := make(chan uuid.UUID, 2)
	release := make(chan struct{})
	publish(t, f.broker, job(1), job(2))
	run := runBroker(t, f.broker, func(ctx context.Context, id uuid.UUID) error {
		entered <- id
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	select {
	case id := <-entered:
		if id != job(1) {
			t.Fatalf("first job=%s", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first job did not start")
	}
	f.broker.Drain()
	select {
	case <-run.done:
		t.Fatal("drain interrupted running job")
	default:
	}
	close(release)
	select {
	case <-run.done:
		if run.err != nil {
			t.Fatal(run.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not finish")
	}
	stream, err := f.js.Stream(t.Context(), f.config.Stream)
	if err != nil || stream.CachedInfo().State.Msgs != 1 {
		t.Fatalf("queued work lost: err=%v", err)
	}
	replacement := natsbroker.New(f.config, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		_ = replacement.Close()
	})
	runBroker(t, replacement, func(_ context.Context, id uuid.UUID) error {
		entered <- id
		return nil
	})
	select {
	case id := <-entered:
		if id != job(2) {
			t.Fatalf("replacement job=%s", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued job not delivered")
	}
	empty(t, f)
}

func TestFailedHandlerRetriesWithoutLosingJob(t *testing.T) {
	f := newFixture(t, 1)
	var calls atomic.Int32
	runBroker(t, f.broker, func(context.Context, uuid.UUID) error {
		if calls.Add(1) == 1 {
			return errors.New("temporary SQL error")
		}
		return nil
	})
	publish(t, f.broker, job(1))
	await(t, func() bool {
		return calls.Load() == 2
	})
	empty(t, f)
}

func TestRepublishingAcknowledgedJobIDIsDeliveredAgain(t *testing.T) {
	f := newFixture(t, 1)
	var calls atomic.Int32
	runBroker(t, f.broker, func(context.Context, uuid.UUID) error {
		calls.Add(1)
		return nil
	})
	publish(t, f.broker, job(1))
	await(t, func() bool {
		return calls.Load() == 1
	})
	empty(t, f)
	// SQL recovery republishes the same ID. A broker-level dedupe window must
	// not suppress this new delivery after the first message was acknowledged.
	publish(t, f.broker, job(1))
	await(t, func() bool {
		return calls.Load() == 2
	})
	empty(t, f)
}

func TestDuplicateDeliveriesSupportIdempotentHandler(t *testing.T) {
	f := newFixture(t, 2)
	var mu sync.Mutex
	closed := make(map[uuid.UUID]bool)
	var calls, executed atomic.Int32
	runBroker(t, f.broker, func(_ context.Context, id uuid.UUID) error {
		mu.Lock()
		defer mu.Unlock()
		calls.Add(1)
		// This models the SQL claim/closed-job boundary. The transport is at
		// least once; application integration tests exercise the real SQL claim.
		if !closed[id] {
			closed[id] = true
			executed.Add(1)
		}
		return nil
	})
	publish(t, f.broker, job(1), job(1))
	await(t, func() bool {
		return calls.Load() == 2
	})
	empty(t, f)
	if executed.Load() != 1 {
		t.Fatalf("executions=%d", executed.Load())
	}
}

func TestProgressHeartbeatPreventsRedeliveryWhileHandlerRuns(t *testing.T) {
	f := newFixture(t, 1)
	consumer, err := f.js.Consumer(t.Context(), f.config.Stream, f.config.Consumer)
	if err != nil {
		t.Fatal(err)
	}
	info, err := consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	config := info.Config
	config.AckWait = 150 * time.Millisecond
	if _, err = f.js.UpdateConsumer(t.Context(), f.config.Stream, config); err != nil {
		t.Fatal(err)
	}
	second := natsbroker.New(f.config, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		_ = second.Close()
	})
	var calls atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	handle := func(ctx context.Context, _ uuid.UUID) error {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	runBroker(t, f.broker, handle)
	runBroker(t, second, handle)
	publish(t, f.broker, job(1))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}
	time.Sleep(700 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("active delivery was redelivered %d times", calls.Load())
	}
	close(release)
	empty(t, f)
}

func TestWorkerDoesNotCreateMissingStream(t *testing.T) {
	f := newFixture(t, 1)
	if err := f.js.DeleteStream(t.Context(), f.config.Stream); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if err := f.broker.Run(ctx, func(context.Context, uuid.UUID) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.js.Stream(t.Context(), f.config.Stream); !errors.Is(err, jetstream.ErrStreamNotFound) {
		t.Fatalf("worker created stream: %v", err)
	}
}

func TestIdleConsumerStaysReadyAcrossFetchTimeouts(t *testing.T) {
	f := newFixture(t, 1)
	run := runBroker(t, f.broker, func(context.Context, uuid.UUID) error {
		t.Error("empty stream delivered work")
		return nil
	})
	await(t, f.broker.Connected)
	deadline := time.NewTimer(2500 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !f.broker.Connected() {
				t.Fatal("idle consumer lost readiness")
			}
		case <-deadline.C:
			f.broker.Drain()
			select {
			case <-run.done:
				if run.err != nil {
					t.Fatal(run.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("idle drain exceeded fetch bound")
			}
			return
		}
	}
}

package rabbitmq

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	amqp "github.com/rabbitmq/amqp091-go"
)

type sessionAcknowledgements struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (a *sessionAcknowledgements) record(call string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, call)
	return a.err
}

func (a *sessionAcknowledgements) Ack(uint64, bool) error { return a.record("ack") }
func (a *sessionAcknowledgements) Nack(uint64, bool, bool) error {
	return a.record("nack")
}
func (a *sessionAcknowledgements) Reject(uint64, bool) error { return a.record("reject") }

func (a *sessionAcknowledgements) count(call string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	var count int
	for _, got := range a.calls {
		if got == call {
			count++
		}
	}
	return count
}

func sessionDelivery(ack amqp.Acknowledger) amqp.Delivery {
	return amqp.Delivery{Acknowledger: ack, Body: []byte(testJobID.String())}
}

func TestUnexpectedDeliveryClosureStopsSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("", "", 2, slog.New(slog.DiscardHandler))
		deliveries := make(chan amqp.Delivery)
		var transportClosed atomic.Bool
		done := make(chan error, 1)
		go func() {
			done <- broker.consumeSession(t.Context(), deliveries,
				func(context.Context, uuid.UUID) error { return nil }, consumerNotifications{},
				func() error { t.Error("unexpected closure attempted graceful drain"); return nil },
				func() { transportClosed.Store(true) })
		}()
		synctest.Wait()
		if !broker.Connected() {
			t.Fatal("consumer was not ready before closure")
		}
		close(deliveries)
		err := <-done
		if err == nil || !strings.Contains(err.Error(), "deliveries closed unexpectedly") {
			t.Fatalf("consumer closure = %v", err)
		}
		if broker.Connected() || !transportClosed.Load() {
			t.Fatal("closed consumer remained ready or kept its transport open")
		}
	})
}

func TestConsumerNotificationsPreserveInFlightHandler(t *testing.T) {
	for _, notification := range []string{"connection", "channel", "consumer"} {
		t.Run(notification, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				broker := New("", "", 1, slog.New(slog.DiscardHandler))
				deliveries := make(chan amqp.Delivery, 1)
				ack := &sessionAcknowledgements{}
				deliveries <- sessionDelivery(ack)
				connectionClosed, channelClosed := make(chan *amqp.Error, 1), make(chan *amqp.Error, 1)
				canceled := make(chan string, 1)
				entered := make(chan struct{})
				release := make(chan struct{})
				var transportClosed atomic.Bool
				done := make(chan error, 1)
				go func() {
					done <- broker.consumeSession(t.Context(), deliveries,
						func(ctx context.Context, _ uuid.UUID) error {
							close(entered)
							select {
							case <-release:
								return nil
							case <-ctx.Done():
								t.Error("transport loss canceled a valid SQL owner before recovery grace")
								return context.Cause(ctx)
							}
						}, consumerNotifications{connectionClosed, channelClosed, canceled},
						func() error { t.Error("unexpected closure attempted graceful drain"); return nil },
						func() { transportClosed.Store(true) })
				}()
				<-entered
				failure := &amqp.Error{Code: 404, Reason: "queue disappeared"}
				switch notification {
				case "connection":
					connectionClosed <- failure
				case "channel":
					channelClosed <- failure
				case "consumer":
					canceled <- consumerTag
				}
				synctest.Wait()
				if broker.Connected() || !transportClosed.Load() {
					t.Fatal("transport loss did not close readiness and intake immediately")
				}
				// Buffered delivery must not be started while the existing owner
				// settles in SQL. The mock ACK succeeds even after transport close.
				deliveries <- sessionDelivery(ack)
				close(release)
				err := <-done
				if err == nil || (notification != "consumer" && !errors.Is(err, failure)) {
					t.Fatalf("notification %s lost its cause: %v", notification, err)
				}
				if notification == "consumer" && !strings.Contains(err.Error(), "canceled by broker") {
					t.Fatalf("consumer cancellation = %v", err)
				}
				if ack.count("ack") != 1 || ack.count("nack") != 0 || len(deliveries) != 1 {
					t.Fatal("transport loss aborted durable settlement or started queued work")
				}
			})
		})
	}
}

func TestTransportRecoveryGraceCancelsAndBoundsStuckHandlers(t *testing.T) {
	for _, stuck := range []bool{false, true} {
		t.Run(map[bool]string{false: "cooperative", true: "stuck"}[stuck], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				broker := New("", "", 1, slog.New(slog.DiscardHandler))
				deliveries := make(chan amqp.Delivery, 1)
				deliveries <- sessionDelivery(&sessionAcknowledgements{})
				entered, release := make(chan struct{}), make(chan struct{})
				closed := make(chan *amqp.Error)
				done := make(chan error, 1)
				go func() {
					done <- broker.consumeSession(t.Context(), deliveries, func(ctx context.Context, _ uuid.UUID) error {
						close(entered)
						<-ctx.Done()
						if stuck {
							<-release
						}
						return context.Cause(ctx)
					}, consumerNotifications{connectionClosed: closed}, func() error { return nil }, func() {})
				}()
				<-entered
				started := time.Now()
				close(closed)
				err := <-done
				if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errConsumerHandlersStuck) != stuck {
					t.Fatalf("recovery result = %v, stuck=%v", err, stuck)
				}
				want := consumerRecoveryGrace
				if stuck {
					want += connectionTimeout
				}
				if elapsed := time.Since(started); elapsed != want {
					t.Fatalf("recovery took %s, want %s", elapsed, want)
				}
				close(release)
			})
		})
	}
}

func TestReconnectBackoffResetsOnlyAfterStableSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("", "", 1, slog.New(slog.DiscardHandler))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var starts []time.Time
		err := broker.run(ctx, func(context.Context, uuid.UUID) error { return nil }, func(context.Context, Handler) error {
			starts = append(starts, time.Now())
			switch len(starts) {
			case 3:
				time.Sleep(stableSessionPeriod)
				broker.sessionStable.Store(true)
			case 4:
				cancel()
			}
			return errors.New("test disconnection")
		})
		if err != nil || len(starts) != 4 {
			t.Fatalf("run = %v, attempts=%d", err, len(starts))
		}
		for i, want := range []time.Duration{time.Second, 2 * time.Second, stableSessionPeriod + time.Second} {
			if got := starts[i+1].Sub(starts[i]); got != want {
				t.Errorf("retry interval %d = %s, want %s", i, got, want)
			}
		}
	})
}

func TestStuckSessionDoesNotReconnect(t *testing.T) {
	broker := New("", "", 1, slog.New(slog.DiscardHandler))
	calls := 0
	err := broker.run(t.Context(), func(context.Context, uuid.UUID) error { return nil }, func(context.Context, Handler) error {
		calls++
		return errConsumerHandlersStuck
	})
	if !errors.Is(err, errConsumerHandlersStuck) || calls != 1 {
		t.Fatalf("stuck session = %v, attempts=%d", err, calls)
	}
}

func TestDrainPreservesInFlightHandlersAndAcknowledgements(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("", "", 2, slog.New(slog.DiscardHandler))
		ack := &sessionAcknowledgements{}
		deliveries := make(chan amqp.Delivery, 3)
		for range 3 {
			deliveries <- sessionDelivery(ack)
		}
		entered, release := make(chan struct{}, 3), make(chan struct{})
		var calls atomic.Int32
		var transportClosed, intakeStopped atomic.Bool
		done := make(chan error, 1)
		go func() {
			done <- broker.consumeSession(t.Context(), deliveries,
				func(ctx context.Context, _ uuid.UUID) error {
					calls.Add(1)
					entered <- struct{}{}
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						t.Error("graceful drain canceled an active job")
						return context.Cause(ctx)
					}
				}, consumerNotifications{}, func() error {
					intakeStopped.Store(true)
					close(deliveries)
					return nil
				}, func() { transportClosed.Store(true) })
		}()
		<-entered
		<-entered
		broker.Drain()
		synctest.Wait()
		if !intakeStopped.Load() || transportClosed.Load() || broker.Connected() || ack.count("ack") != 0 {
			t.Fatal("drain must stop intake/readiness while preserving the active acknowledgement channel")
		}
		select {
		case err := <-done:
			t.Fatalf("drain finished before active jobs: %v", err)
		default:
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 || ack.count("ack") != 2 || transportClosed.Load() {
			t.Fatal("drain lost an acknowledgement or started queued work")
		}
	})
}

func TestDrainCancellationRPCIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("", "", 1, slog.New(slog.DiscardHandler))
		deliveries := make(chan amqp.Delivery, 1)
		deliveries <- sessionDelivery(&sessionAcknowledgements{})
		entered, transportClosed, cancelStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- broker.consumeSession(t.Context(), deliveries,
				func(ctx context.Context, _ uuid.UUID) error {
					close(entered)
					<-ctx.Done()
					return context.Cause(ctx)
				}, consumerNotifications{}, func() error {
					close(cancelStarted)
					<-transportClosed
					return errors.New("transport closed")
				}, func() { close(transportClosed) })
		}()
		<-entered
		broker.Drain()
		<-cancelStarted
		synctest.Sleep(connectionTimeout)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked cancellation did not time out: %v", err)
		}
		if broker.Connected() {
			t.Fatal("failed drain retained readiness")
		}
	})
}

func TestSessionPreservesSettlementFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("ack transport failure")
		broker := New("", "", 1, slog.New(slog.DiscardHandler))
		deliveries := make(chan amqp.Delivery, 1)
		deliveries <- sessionDelivery(&sessionAcknowledgements{err: failure})
		err := broker.consumeSession(t.Context(), deliveries,
			func(context.Context, uuid.UUID) error { return nil }, consumerNotifications{},
			func() error { return nil }, func() {})
		if !errors.Is(err, failure) {
			t.Fatalf("settlement cause lost at supervisor: %v", err)
		}
	})
}

func TestInvalidDeliveryRejectionFailureIsReturned(t *testing.T) {
	failure := errors.New("reject transport failure")
	broker := New("", "", 1, nil)
	err := broker.settleDelivery(t.Context(), amqp.Delivery{
		Acknowledger: &sessionAcknowledgements{err: failure}, Body: []byte("invalid"),
	}, func(context.Context, uuid.UUID) error { t.Fatal("invalid delivery reached handler"); return nil })
	if !errors.Is(err, failure) {
		t.Fatalf("reject failure = %v", err)
	}
}

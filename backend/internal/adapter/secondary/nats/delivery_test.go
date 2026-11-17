package natsbroker

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"uuid"
)

type testDelivery struct {
	value      []byte
	acked      atomic.Int32
	nacked     atomic.Int32
	terminated atomic.Int32
	progress   atomic.Int32
	ackErr     error
	nakErr     error
}

func (d *testDelivery) Data() []byte {
	return d.value
}
func (d *testDelivery) DoubleAck(context.Context) error {
	d.acked.Add(1)
	return d.ackErr
}
func (d *testDelivery) NakWithDelay(time.Duration) error {
	d.nacked.Add(1)
	return d.nakErr
}
func (d *testDelivery) Term() error {
	d.terminated.Add(1)
	return nil
}
func (d *testDelivery) InProgress() error {
	d.progress.Add(1)
	return nil
}

func unitBroker() *Broker {
	return New(Config{
		URL:         "nats://127.0.0.1:1",
		Stream:      "TEST",
		Subject:     "test.jobs",
		Consumer:    "workers",
		Concurrency: 1,
	}, slog.New(slog.DiscardHandler))
}

func TestDeliveryAcknowledgesOnlyAfterHandlerAndKeepsProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := unitBroker()
		id := uuid.MustParse("00000000-0000-7000-8000-000000000001")
		msg := &testDelivery{
			value: []byte(id.String()),
		}
		err := b.settleDelivery(t.Context(), msg, func(_ context.Context, got uuid.UUID) error {
			if got != id || msg.acked.Load() != 0 {
				t.Error("delivery acknowledged before settlement")
			}
			time.Sleep(250 * time.Millisecond)
			return nil
		}, 50*time.Millisecond)
		if err != nil || msg.acked.Load() != 1 || msg.progress.Load() < 3 {
			t.Fatalf("err=%v ack=%d progress=%d", err, msg.acked.Load(), msg.progress.Load())
		}
		progress := msg.progress.Load()
		time.Sleep(time.Second)
		if msg.progress.Load() != progress {
			t.Fatal("heartbeat survived final acknowledgement")
		}
	})
}

func TestDeliveryRetriesUnsettledJobAndRejectsMalformedID(t *testing.T) {
	b := unitBroker()
	id := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	msg := &testDelivery{
		value: []byte(id.String()),
	}
	if err := b.settleDelivery(t.Context(), msg, func(context.Context, uuid.UUID) error {
		return errors.New("SQL unavailable")
	}, time.Second); err != nil {
		t.Fatal(err)
	}
	if msg.nacked.Load() != 1 || msg.acked.Load() != 0 {
		t.Fatal("unsettled job was acknowledged")
	}
	invalid := &testDelivery{
		value: []byte("not-a-job"),
	}
	if err := b.settleDelivery(t.Context(), invalid, func(context.Context, uuid.UUID) error {
		t.Fatal("invalid ID reached handler")
		return nil
	}, time.Second); err != nil {
		t.Fatal(err)
	}
	if invalid.terminated.Load() != 1 || invalid.nacked.Load() != 0 {
		t.Fatal("invalid job was not terminated")
	}
}

func TestAcknowledgementFailureIsNotSuccessfulSettlement(t *testing.T) {
	b := unitBroker()
	failed := errors.New("ack transport lost")
	msg := &testDelivery{
		value:  []byte("00000000-0000-7000-8000-000000000001"),
		ackErr: failed,
	}
	err := b.settleDelivery(t.Context(), msg, func(context.Context, uuid.UUID) error {
		return nil
	}, time.Second)
	if !errors.Is(err, failed) {
		t.Fatalf("lost acknowledgement err=%v", err)
	}
}

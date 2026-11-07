package natsbroker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/nats-io/nats.go/jetstream"
)

type transportError struct {
	detail string
}

func (failure *transportError) Error() string {
	return failure.detail
}

func assertErrorChain(
	t *testing.T,
	err, cause error,
	locations ...string,
) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("cause lost: err=%v cause=%v", err, cause)
	}
	previous := -1
	for _, location := range locations {
		index := strings.Index(err.Error(), location)
		if index <= previous {
			t.Fatalf("missing or out-of-order location %q in %v", location, err)
		}
		previous = index
	}
}

func TestConnectionCancellationIncludesNestedLocations(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	b := unitBroker()
	err := b.Ensure(ctx)
	assertErrorChain(t, err, context.Canceled, "natsbroker.Broker.Ensure", "natsbroker.Broker.connection")
	_, err = b.publishJob(ctx, nil, uuid.MustParse("00000000-0000-7000-8000-000000000001"))
	assertErrorChain(t, err, context.Canceled, "natsbroker.Broker.publishJob")
}

func TestDeliveryLocationsPreserveTypedAcknowledgementError(t *testing.T) {
	b := unitBroker()
	cause := &transportError{
		detail: "ack transport disconnected",
	}
	consumer := &testConsumer{
		queue: make(chan jetstream.Msg, 1),
	}
	consumer.queue <- testMsg{
		testDelivery: &testDelivery{
			value:  []byte("00000000-0000-7000-8000-000000000001"),
			ackErr: cause,
		},
	}
	err := b.consumeDeliveries(t.Context(), t.Context(), consumer, func(context.Context, uuid.UUID) error {
		return nil
	}, time.Hour)
	assertErrorChain(t, err, cause, "natsbroker.Broker.consumeDeliveries", "natsbroker.Broker.settleDelivery")
	target, ok := errors.AsType[*transportError](err)
	if !ok || target != cause {
		t.Fatalf("typed error lost: %v", err)
	}
}

func TestPublicationLocationsPreserveConfirmationError(t *testing.T) {
	id := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	cause := &transportError{
		detail: "publication rejected",
	}
	_, err := publishConfirmedBatch(t.Context(), []uuid.UUID{id}, "TEST", func(uuid.UUID) (jetstream.PubAckFuture, error) {
		return future(nil, cause), nil
	})
	assertErrorChain(t, err, cause, "natsbroker.publishConfirmedBatch", "natsbroker.waitForConfirmation")
}

type failingConsumer struct {
	err error
}

func (consumer failingConsumer) Fetch(_ int, _ ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
	return nil, consumer.err
}

func TestSessionLocationsPreserveDeliveryFailure(t *testing.T) {
	b := unitBroker()
	cause := &transportError{
		detail: "consumer was deleted",
	}
	err := b.consumeSession(t.Context(), failingConsumer{
		err: cause,
	}, func(context.Context, uuid.UUID) error {
		return nil
	}, time.Hour)
	assertErrorChain(t, err, cause, "natsbroker.Broker.consumeSession", "natsbroker.Broker.startSessionWorkers", "natsbroker.Broker.consumeDeliveries")
}

func TestSessionFinishPreservesCustomContextCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("worker lease was revoked")
	cancel(cause)
	done := make(chan struct{})
	session := consumerSession{
		done: done,
		cancelWork: func(error) {
			close(done)
		},
	}
	err := session.finish(ctx, 0)
	assertErrorChain(t, err, cause, "natsbroker.consumerSession.finish")
}

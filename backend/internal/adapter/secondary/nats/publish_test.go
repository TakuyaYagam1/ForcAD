package natsbroker

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type testFuture struct {
	ok  chan *jetstream.PubAck
	err chan error
}

func (f testFuture) Ok() <-chan *jetstream.PubAck {
	return f.ok
}
func (f testFuture) Err() <-chan error {
	return f.err
}
func (f testFuture) Msg() *nats.Msg {
	return nil
}
func future(ack *jetstream.PubAck, err error) testFuture {
	f := testFuture{
		ok:  make(chan *jetstream.PubAck, 1),
		err: make(chan error, 1),
	}
	if ack != nil {
		f.ok <- ack
	}
	if err != nil {
		f.err <- err
	}
	return f
}

func TestPublicationReturnsOnlyConfirmedIDsIncludingPartialSuccess(t *testing.T) {
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-7000-8000-000000000001"), uuid.MustParse("00000000-0000-7000-8000-000000000002"),
		uuid.MustParse("00000000-0000-7000-8000-000000000003"), uuid.MustParse("00000000-0000-7000-8000-000000000004"),
	}
	failed := errors.New("publication rejected")
	i := 0
	got, err := publishConfirmedBatch(t.Context(), ids, "TEST", func(uuid.UUID) (jetstream.PubAckFuture, error) {
		i++
		switch i {
		case 2:
			return future(nil, failed), nil
		case 4:
			return nil, failed
		default:
			return future(&jetstream.PubAck{
				Stream:   "TEST",
				Sequence: uint64(i),
			}, nil), nil
		}
	})
	if !errors.Is(err, failed) || !slices.Equal(got, []uuid.UUID{ids[0], ids[2]}) {
		t.Fatalf("confirmed=%v err=%v", got, err)
	}
}

func TestPublicationPreservesAvailableConfirmationAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f := future(&jetstream.PubAck{
		Stream:   "TEST",
		Sequence: 1,
	}, nil)
	ack, err := waitForConfirmation(ctx, f)
	if err != nil || ack == nil {
		t.Fatalf("confirmation lost after cancellation: %v", err)
	}
	_, err = waitForConfirmation(ctx, future(nil, nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unconfirmed publication err=%v", err)
	}
}

func TestLazyBrokerRejectsInvalidBatchWithoutConnecting(t *testing.T) {
	b := unitBroker()
	if b.client != nil || b.Connected() {
		t.Fatal("constructor connected to NATS")
	}
	if _, err := b.Publish(t.Context(), []uuid.UUID{uuid.Nil()}); err == nil {
		t.Fatal("nil ID accepted")
	}
	if _, err := b.Publish(t.Context(), make([]uuid.UUID, maxPublicationBatch+1)); err == nil {
		t.Fatal("oversized batch accepted")
	}
	if b.client != nil {
		t.Fatal("invalid batch opened connection")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

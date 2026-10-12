package rabbitmq

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	amqp "github.com/rabbitmq/amqp091-go"
)

var testJobID = uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")

func TestParseJobIDRequiresCanonicalNonNilUUID(t *testing.T) {
	id, err := parseJobID(testJobID.String())
	if err != nil || id != testJobID {
		t.Fatalf("parse canonical UUID = %s, %v; want %s", id, err, testJobID)
	}
	for _, value := range []string{
		"", "../job", "123", "11111111111141118111111111111111",
		strings.ToUpper(testJobID.String()), "{" + testJobID.String() + "}", uuid.Nil().String(),
	} {
		if _, err := parseJobID(value); err == nil {
			t.Errorf("accepted noncanonical or nil job ID %q", value)
		}
	}
	var _ amqp.Acknowledger = (*amqp.Channel)(nil)
}

type deliveryAcknowledgements struct {
	calls []string
	err   error
}

func (a *deliveryAcknowledgements) Ack(_ uint64, _ bool) error {
	a.calls = append(a.calls, "ack")
	return a.err
}

func (a *deliveryAcknowledgements) Nack(_ uint64, _ bool, requeue bool) error {
	if requeue {
		a.calls = append(a.calls, "requeue")
	} else {
		a.calls = append(a.calls, "nack")
	}
	return a.err
}

func (a *deliveryAcknowledgements) Reject(_ uint64, requeue bool) error {
	if requeue {
		a.calls = append(a.calls, "reject-and-requeue")
	} else {
		a.calls = append(a.calls, "reject")
	}
	return a.err
}

func TestDeliveryAcknowledgedOnlyAfterHandlerSettles(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "retry"}[retry], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			acknowledgements := &deliveryAcknowledgements{}
			broker := New("", "", 1, slog.New(slog.DiscardHandler))
			delivery := amqp.Delivery{Acknowledger: acknowledgements, Body: []byte(testJobID.String())}
			err := broker.settleDelivery(ctx, delivery, func(_ context.Context, id uuid.UUID) error {
				if id != testJobID {
					t.Fatalf("handler received job ID %s, want %s", id, testJobID)
				}
				if len(acknowledgements.calls) != 0 {
					t.Fatal("message acknowledged before handler finished")
				}
				acknowledgements.calls = append(acknowledgements.calls, "handle")
				if retry {
					cancel()
					return errors.New("storage unavailable")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"handle", "ack"}
			if retry {
				want[1] = "requeue"
			}
			if !reflect.DeepEqual(acknowledgements.calls, want) {
				t.Fatalf("delivery lifecycle = %v, want %v", acknowledgements.calls, want)
			}
		})
	}
}

func TestInvalidDeliveryIsRejectedWithoutRunningChecker(t *testing.T) {
	for _, value := range []string{"invalid", uuid.Nil().String(), "11111111111141118111111111111111"} {
		t.Run(value, func(t *testing.T) {
			acknowledgements := &deliveryAcknowledgements{}
			delivery := amqp.Delivery{Acknowledger: acknowledgements, Body: []byte(value)}
			broker := New("", "", 1, nil)
			if err := broker.settleDelivery(t.Context(), delivery, func(_ context.Context, _ uuid.UUID) error {
				t.Fatal("invalid delivery reached the checker")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(acknowledgements.calls, []string{"reject"}) {
				t.Fatalf("invalid delivery acknowledgement = %v", acknowledgements.calls)
			}
		})
	}
}

func TestDeliveryAcknowledgementFailurePreservesCause(t *testing.T) {
	failure := errors.New("channel closed")
	delivery := amqp.Delivery{
		Acknowledger: &deliveryAcknowledgements{err: failure},
		Body:         []byte(testJobID.String()),
	}
	broker := New("", "", 1, nil)
	err := broker.settleDelivery(t.Context(), delivery, func(_ context.Context, _ uuid.UUID) error { return nil })
	if !errors.Is(err, failure) {
		t.Fatalf("acknowledgement error = %v, want %v", err, failure)
	}
}

func TestPublishRejectsOversizedBatchWithContext(t *testing.T) {
	broker := New("", "", 1, nil)
	_, err := broker.Publish(context.Background(), make([]uuid.UUID, 257))
	if err == nil || !strings.HasPrefix(err.Error(), "rabbitmq.Broker.Publish - ") {
		t.Fatalf("Publish() error = %v, want local context", err)
	}
}

func TestPublishRejectsNilJobIDBeforeConnecting(t *testing.T) {
	broker := New("", "", 1, nil)
	_, err := broker.Publish(context.Background(), []uuid.UUID{uuid.Nil()})
	if err == nil || !strings.HasPrefix(err.Error(), "rabbitmq.Broker.Publish - ") {
		t.Fatalf("Publish() error = %v, want local nil-ID error", err)
	}
}

type confirmationStub struct {
	done        chan struct{}
	acked       bool
	waitStarted chan struct{}
	waitOnce    sync.Once
}

func newConfirmationStub(acked, complete bool) *confirmationStub {
	confirmation := &confirmationStub{done: make(chan struct{})}
	if complete {
		confirmation.settle(acked)
	}
	return confirmation
}

func (c *confirmationStub) WaitContext(ctx context.Context) (bool, error) {
	if c.waitStarted != nil {
		c.waitOnce.Do(func() { close(c.waitStarted) })
	}
	select {
	case <-c.done:
		return c.acked, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (c *confirmationStub) Done() <-chan struct{} { return c.done }

func (c *confirmationStub) Acked() bool {
	select {
	case <-c.done:
		return c.acked
	default:
		return false
	}
}

func (c *confirmationStub) settle(acked bool) {
	c.acked = acked
	close(c.done)
}

func TestPublishConfirmedBatchPipelinesAndCollectsAcksAfterErrors(t *testing.T) {
	ids := []uuid.UUID{
		testJobID,
		uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
		uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
	}
	nack := newConfirmationStub(false, true)
	ackAfterError := newConfirmationStub(false, false)
	ackAfterError.waitStarted = make(chan struct{})
	publishFailure := errors.New("channel closed during publish")
	attempted := make([]uuid.UUID, 0, len(ids))
	done := make(chan struct {
		confirmed []uuid.UUID
		err       error
	}, 1)
	go func() {
		confirmed, err := publishConfirmedBatch(t.Context(), ids, func(_ context.Context, id uuid.UUID) (publicationConfirmation, error) {
			attempted = append(attempted, id)
			switch id {
			case ids[0]:
				return nack, nil
			case ids[1]:
				return ackAfterError, nil
			default:
				return nil, publishFailure
			}
		})
		done <- struct {
			confirmed []uuid.UUID
			err       error
		}{confirmed, err}
	}()
	select {
	case <-ackAfterError.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("batch did not wait for the second confirmation")
	}
	if !reflect.DeepEqual(attempted, ids) {
		t.Fatalf("publish attempts before waiting = %v, want %v", attempted, ids)
	}
	select {
	case result := <-done:
		t.Fatalf("batch returned before the pending ack: %v %v", result.confirmed, result.err)
	default:
	}
	ackAfterError.settle(true)
	result := <-done
	if !reflect.DeepEqual(result.confirmed, ids[1:2]) {
		t.Fatalf("confirmed IDs = %v, want only %v", result.confirmed, ids[1:2])
	}
	if !errors.Is(result.err, publishFailure) || !strings.Contains(result.err.Error(), "not confirmed") {
		t.Fatalf("batch error = %v, want publish failure and earlier Nack", result.err)
	}
}

func TestPublishConfirmedBatchCancellationKeepsOnlyKnownAcks(t *testing.T) {
	ids := []uuid.UUID{
		testJobID,
		uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
		uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
	}
	knownAck := newConfirmationStub(true, true)
	pending := newConfirmationStub(false, false)
	pending.waitStarted = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct {
		confirmed []uuid.UUID
		err       error
	}, 1)
	go func() {
		confirmed, err := publishConfirmedBatch(ctx, ids, func(_ context.Context, id uuid.UUID) (publicationConfirmation, error) {
			if id == ids[0] {
				return knownAck, nil
			}
			return pending, nil
		})
		done <- struct {
			confirmed []uuid.UUID
			err       error
		}{confirmed, err}
	}()
	select {
	case <-pending.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("batch did not wait for pending confirmation")
	}
	cancel()
	result := <-done
	if !reflect.DeepEqual(result.confirmed, ids[:1]) {
		t.Fatalf("confirmed IDs after cancellation = %v, want only %v", result.confirmed, ids[:1])
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("batch error = %v, want context.Canceled", result.err)
	}
}

func TestWaitForConfirmationKeepsAckWhenContextAlsoExpires(t *testing.T) {
	confirmation := newConfirmationStub(true, true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	acked, err := waitForConfirmation(ctx, confirmation)
	if err != nil || !acked {
		t.Fatalf("waitForConfirmation() = %t, %v; want acknowledged", acked, err)
	}
}

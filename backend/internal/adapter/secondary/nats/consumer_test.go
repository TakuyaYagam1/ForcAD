package natsbroker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type testMsg struct {
	*testDelivery
}

func (m testMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return nil, nil
}
func (m testMsg) Headers() nats.Header {
	return nil
}
func (m testMsg) Subject() string {
	return "test.jobs"
}
func (m testMsg) Reply() string {
	return "reply"
}
func (m testMsg) Ack() error {
	return m.DoubleAck(context.Background())
}
func (m testMsg) Nak() error {
	return m.NakWithDelay(0)
}
func (m testMsg) TermWithReason(string) error {
	return m.Term()
}

type testBatch struct {
	messages chan jetstream.Msg
	err      error
}

func (b testBatch) Messages() <-chan jetstream.Msg {
	return b.messages
}
func (b testBatch) Error() error {
	return b.err
}

type testConsumer struct {
	queue chan jetstream.Msg
	pulls atomic.Int32
}

func (c *testConsumer) Fetch(count int, _ ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
	if count != 1 {
		return nil, errors.New("unbounded fetch")
	}
	c.pulls.Add(1)
	batch := testBatch{
		messages: make(chan jetstream.Msg, 1),
	}
	go func() {
		defer close(batch.messages)
		select {
		case msg := <-c.queue:
			batch.messages <- msg
		case <-time.After(fetchWait):
		}
	}()
	return batch, nil
}

func TestSessionPullsOnlyForFreeSlotsAndDrainLeavesQueuedJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := unitBroker()
		b.config.Concurrency = 2
		consumer := &testConsumer{
			queue: make(chan jetstream.Msg, 3),
		}
		for range 3 {
			consumer.queue <- testMsg{&testDelivery{
				value: []byte("00000000-0000-7000-8000-000000000001"),
			}}
		}
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- b.consumeSession(t.Context(), consumer, func(context.Context, uuid.UUID) error {
				entered <- struct{}{}
				<-release
				return nil
			}, time.Second)
		}()
		<-entered
		<-entered
		synctest.Wait()
		if consumer.pulls.Load() != 2 || len(consumer.queue) != 1 {
			t.Fatalf("pulls=%d queue=%d", consumer.pulls.Load(), len(consumer.queue))
		}
		b.Drain()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("drain interrupted running handler")
		default:
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if len(consumer.queue) != 1 {
			t.Fatal("drain reserved queued job")
		}
	})
}

func TestSessionReplacesHandlerThatIgnoresCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := unitBroker()
		ctx, cancel := context.WithCancel(t.Context())
		consumer := &testConsumer{
			queue: make(chan jetstream.Msg, 1),
		}
		consumer.queue <- testMsg{&testDelivery{
			value: []byte("00000000-0000-7000-8000-000000000001"),
		}}
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- b.consumeSession(ctx, consumer, func(context.Context, uuid.UUID) error {
				close(entered)
				<-release
				return nil
			}, time.Second)
		}()
		<-entered
		cancel()
		err := <-done
		if !errors.Is(err, errConsumerHandlersStuck) {
			t.Fatalf("stuck handler err=%v", err)
		}
		close(release)
		synctest.Wait()
	})
}

package rabbitmq_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestConsumerCancellationReconnectsAndResumesDelivery(t *testing.T) {
	address := os.Getenv("FORCAD_TEST_AMQP_URL")
	if address == "" {
		t.Skip("FORCAD_TEST_AMQP_URL is not set")
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Path != "/forcad_test" {
		t.Fatal("integration broker must use forcad_test vhost")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	queue := fmt.Sprintf("test.reconnect.%d", time.Now().UnixNano())
	broker := rabbitmq.New(address, queue, 1, slog.New(slog.DiscardHandler))
	conn, err := amqp.Dial(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseDeadline(time.Now().Add(time.Second)) })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = ch.QueueDelete(queue, false, false, false) })

	received := make(chan uuid.UUID, 1)
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		runErr = broker.Run(ctx, func(ctx context.Context, id uuid.UUID) error {
			select {
			case received <- id:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("consumer did not stop after cancellation")
		}
	})

	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for !broker.Connected() {
		select {
		case <-done:
			t.Fatalf("consumer stopped before connecting: %v", runErr)
		case <-ctx.Done():
			t.Fatal("consumer did not connect")
		case <-poll.C:
		}
	}

	// Delete an empty queue to trigger server-side basic.cancel without an
	// in-flight acknowledgement or a client connection failure.
	if _, err := ch.QueueDelete(queue, false, false, false); err != nil {
		t.Fatal(err)
	}
	// An active declaration recreates the queue and keeps the admin channel
	// usable even if the consumer has not reconnected yet.
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	want := integrationJobIDs[1]
	confirmed, err := broker.Publish(ctx, []uuid.UUID{want})
	if err != nil || len(confirmed) != 1 || confirmed[0] != want {
		t.Fatalf("publication after consumer cancellation: %v %v", confirmed, err)
	}
	select {
	case id := <-received:
		if id != want {
			t.Fatalf("reconnected consumer received %s, want %s", id, want)
		}
	case <-done:
		t.Fatalf("consumer stopped instead of reconnecting: %v", runErr)
	case <-ctx.Done():
		t.Fatal("consumer did not resume delivery after server cancellation")
	}
	if !broker.Connected() {
		t.Fatal("reconnected consumer is not reported as connected")
	}
	inspection, err := ch.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil || inspection.Consumers != 1 {
		t.Fatalf("consumer registration after reconnect: %d %v", inspection.Consumers, err)
	}

	cancel()
	select {
	case <-done:
		if runErr != nil {
			t.Fatalf("consumer cancellation returned an error: %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconnected consumer did not stop after cancellation")
	}
	if broker.Connected() {
		t.Fatal("stopped consumer is still reported as connected")
	}
}

package rabbitmq_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
)

var integrationJobIDs = []uuid.UUID{
	uuid.MustParse("11111111-1111-4111-8111-111111111111"),
	uuid.MustParse("22222222-2222-4222-8222-222222222222"),
}

func TestDrainFinishesCurrentDeliveryAndLeavesQueuedWork(t *testing.T) {
	address := os.Getenv("FORCAD_TEST_AMQP_URL")
	if address == "" {
		t.Skip("FORCAD_TEST_AMQP_URL is not set")
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Path != "/forcad_test" {
		t.Fatal("integration broker must use forcad_test vhost")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	queue := fmt.Sprintf("test.drain.%d", time.Now().UnixNano())
	broker := rabbitmq.New(address, queue, 1, slog.New(slog.DiscardHandler))
	conn, err := amqp.Dial(address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ch.QueuePurge(queue, false); err != nil {
		t.Fatal(err)
	}
	defer ch.QueueDelete(queue, false, false, false)
	confirmed, err := broker.Publish(ctx, integrationJobIDs)
	if err != nil || len(confirmed) != 2 {
		t.Fatalf("publication confirmation: %v %v", confirmed, err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- broker.Run(ctx, func(ctx context.Context, _ uuid.UUID) error {
			if calls.Add(1) == 1 {
				close(entered)
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("consumer did not start")
	}
	broker.Drain()
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("consumer did not drain")
	}
	if calls.Load() != 1 {
		t.Fatalf("drain started %d jobs", calls.Load())
	}
	inspection, err := ch.QueueDeclarePassive(queue, true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Messages != 1 {
		t.Fatalf("queued work after drain: %d", inspection.Messages)
	}
	delivery, ok, err := ch.Get(queue, false)
	if err != nil || !ok || string(delivery.Body) != integrationJobIDs[1].String() {
		t.Fatalf("queued delivery: %s %v %v", delivery.Body, ok, err)
	}
	if err := delivery.Ack(false); err != nil {
		t.Fatal(err)
	}
}

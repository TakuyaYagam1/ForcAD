package rabbitmq_test

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
)

func BenchmarkPublishBatch(b *testing.B) {
	address := os.Getenv("FORCAD_TEST_AMQP_URL")
	if address == "" {
		b.Skip("FORCAD_TEST_AMQP_URL is not set")
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Path != "/forcad_test" {
		b.Fatal("benchmark broker must use the forcad_test vhost")
	}
	for _, size := range []int{20, 100, 256} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			benchmarkPublishBatch(b, address, size)
		})
	}
}

func benchmarkPublishBatch(b *testing.B, address string, size int) {
	b.Helper()
	b.StopTimer()
	queue := fmt.Sprintf("test.publish.batch.%d.%d", os.Getpid(), time.Now().UnixNano())
	connection, err := amqp.Dial(address)
	if err != nil {
		b.Fatal("cannot connect to integration RabbitMQ")
	}
	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		b.Fatalf("open benchmark queue channel: %v", err)
	}
	if _, err := channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		b.Fatalf("declare benchmark queue: %v", err)
	}
	b.Cleanup(func() {
		if _, err := channel.QueueDelete(queue, false, false, false); err != nil {
			b.Errorf("delete benchmark queue: %v", err)
		}
		_ = channel.Close()
		_ = connection.Close()
	})
	broker := rabbitmq.New(address, queue, 1, slog.New(slog.DiscardHandler))
	b.ReportMetric(float64(size), "messages/op")
	for range b.N {
		b.StopTimer()
		if _, err := channel.QueuePurge(queue, false); err != nil {
			b.Fatalf("purge benchmark queue: %v", err)
		}
		ids := make([]uuid.UUID, size)
		for i := range ids {
			ids[i] = uuid.NewV7()
		}
		b.StartTimer()
		confirmed, err := broker.Publish(b.Context(), ids)
		b.StopTimer()
		if err != nil {
			b.Fatalf("publish batch of %d jobs: %v", size, err)
		}
		if len(confirmed) != size {
			b.Fatalf("confirmed %d jobs in a batch of %d", len(confirmed), size)
		}
	}
}

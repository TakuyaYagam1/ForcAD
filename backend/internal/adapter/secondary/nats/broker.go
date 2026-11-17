// Package natsbroker transports durable checker job IDs through NATS JetStream.
// PostgreSQL remains authoritative for job claims, leases and completion.
package natsbroker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	connectionTimeout     = 5 * time.Second
	publicationTimeout    = 10 * time.Second
	ackTimeout            = 5 * time.Second
	ackWait               = 30 * time.Second
	fetchWait             = time.Second
	minRetryDelay         = time.Second
	maxRetryDelay         = 10 * time.Second
	consumerRecoveryGrace = 30 * time.Second
	maxPublicationBatch   = 256
	// Reject at the storage cap so the durable SQL outbox retries; never
	// evict older, still-unacknowledged checker jobs to admit new messages.
	maxStreamMessages = 100000
	maxStreamBytes    = 64 << 20
)

// Config separates the fleet-wide delivery budget from one process's slots.
type Config struct {
	URL           string
	Stream        string
	Subject       string
	Consumer      string
	Concurrency   int
	MaxAckPending int
}

type Broker struct {
	config          Config
	logger          *slog.Logger
	draining        chan struct{}
	drainOnce       sync.Once
	connected       atomic.Bool
	mu              sync.Mutex
	client          *client
	closed          bool
	publicationGate chan struct{}
}

func New(config Config, logger *slog.Logger) *Broker {
	if logger == nil {
		logger = slog.Default()
	}
	if config.MaxAckPending == 0 {
		config.MaxAckPending = 1024
	}
	return &Broker{
		config:          config,
		logger:          logger,
		draining:        make(chan struct{}),
		publicationGate: make(chan struct{}, 1),
	}
}

func (b *Broker) Drain() {
	b.drainOnce.Do(func() {
		close(b.draining)
		b.connected.Store(false)
	})
}

func (b *Broker) Connected() bool {
	return !b.isDraining() && b.connected.Load() && b.transportConnected()
}

func (b *Broker) isDraining() bool {
	select {
	case <-b.draining:
		return true
	default:
		return false
	}
}

func (b *Broker) validate() error {
	const location = "natsbroker.Broker.validate"
	if strings.TrimSpace(b.config.URL) == "" {
		return fmt.Errorf("%s - NATS URL is required", location)
	}
	for _, value := range []string{b.config.Stream, b.config.Consumer} {
		if value == "" || strings.ContainsAny(value, ".*>/\\") || strings.ContainsFunc(value, unicode.IsSpace) || strings.ContainsFunc(value, unicode.IsControl) {
			return fmt.Errorf("%s - JetStream stream and consumer names must be nonempty names without wildcards or separators", location)
		}
	}
	if b.config.Subject == "" || strings.ContainsAny(b.config.Subject, "*>") || strings.ContainsFunc(b.config.Subject, unicode.IsSpace) || strings.ContainsFunc(b.config.Subject, unicode.IsControl) {
		return fmt.Errorf("%s - checker subject must be a concrete NATS subject", location)
	}
	for _, token := range strings.Split(b.config.Subject, ".") {
		if token == "" {
			return fmt.Errorf("%s - checker subject has an empty token", location)
		}
	}
	if b.config.Concurrency < 1 || b.config.MaxAckPending < b.config.Concurrency {
		return fmt.Errorf("%s - checker concurrency must be positive and fit the fleet MaxAckPending budget", location)
	}
	return nil
}

// Ensure creates absent resources and validates existing ones. Only publishers
// and setup call it; workers bind without rewriting shared settings.
func (b *Broker) Ensure(ctx context.Context) error {
	const location = "natsbroker.Broker.Ensure"
	if err := b.validate(); err != nil {
		return fmt.Errorf("%s - validate configuration: %w", location, err)
	}
	c, err := b.connection(ctx)
	if err != nil {
		return fmt.Errorf("%s - connect: %w", location, err)
	}
	stream, err := c.js.Stream(ctx, b.config.Stream)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		stream, err = c.js.CreateStream(ctx, jetstream.StreamConfig{
			Name:         b.config.Stream,
			Subjects:     []string{b.config.Subject},
			Storage:      jetstream.FileStorage,
			Retention:    jetstream.WorkQueuePolicy,
			Discard:      jetstream.DiscardNew,
			MaxMsgs:      maxStreamMessages,
			MaxBytes:     maxStreamBytes,
			MaxMsgSize:   1024,
			MaxConsumers: 1,
			Replicas:     1,
		})
		if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
			stream, err = c.js.Stream(ctx, b.config.Stream)
		}
	}
	if err != nil {
		return fmt.Errorf("%s - prepare checker stream: %w", location, err)
	}
	if err := b.validateStream(stream.CachedInfo().Config); err != nil {
		return fmt.Errorf("%s - validate checker stream: %w", location, err)
	}
	consumer, err := c.js.Consumer(ctx, b.config.Stream, b.config.Consumer)
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		consumer, err = c.js.CreateConsumer(ctx, b.config.Stream, jetstream.ConsumerConfig{
			Name:              b.config.Consumer,
			Durable:           b.config.Consumer,
			FilterSubject:     b.config.Subject,
			AckPolicy:         jetstream.AckExplicitPolicy,
			AckWait:           ackWait,
			MaxDeliver:        -1,
			MaxAckPending:     b.config.MaxAckPending,
			MaxWaiting:        b.config.MaxAckPending,
			MaxRequestBatch:   1,
			MaxRequestExpires: 2 * fetchWait,
		})
		if errors.Is(err, jetstream.ErrConsumerExists) {
			consumer, err = c.js.Consumer(ctx, b.config.Stream, b.config.Consumer)
		}
	}
	if err != nil {
		return fmt.Errorf("%s - prepare checker consumer: %w", location, err)
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("%s - inspect checker consumer: %w", location, err)
	}
	if err := b.validateConsumer(info.Config); err != nil {
		return fmt.Errorf("%s - validate checker consumer: %w", location, err)
	}
	b.connected.Store(true)
	return nil
}

func (b *Broker) validateStream(config jetstream.StreamConfig) error {
	const location = "natsbroker.Broker.validateStream"
	if config.Storage != jetstream.FileStorage || config.Retention != jetstream.WorkQueuePolicy || config.Discard != jetstream.DiscardNew || config.NoAck ||
		len(config.Subjects) != 1 || config.Subjects[0] != b.config.Subject || config.MaxAge != 0 || config.Sealed {
		return fmt.Errorf("%s - existing checker stream has incompatible persistence, retention or subject settings", location)
	}
	return nil
}

func (b *Broker) validateConsumer(config jetstream.ConsumerConfig) error {
	const location = "natsbroker.Broker.validateConsumer"
	if !b.compatibleConsumerIdentity(config) || !compatibleConsumerDelivery(config) || !b.compatibleConsumerPulls(config) {
		return fmt.Errorf("%s - existing checker consumer has incompatible durable pull, acknowledgement or fleet capacity settings", location)
	}
	return nil
}

func (b *Broker) compatibleConsumerIdentity(config jetstream.ConsumerConfig) bool {
	return config.Durable == b.config.Consumer && config.DeliverSubject == "" &&
		config.FilterSubject == b.config.Subject && len(config.FilterSubjects) == 0
}

func compatibleConsumerDelivery(config jetstream.ConsumerConfig) bool {
	return config.AckPolicy == jetstream.AckExplicitPolicy && config.AckWait >= 3*time.Millisecond &&
		config.MaxDeliver == -1 && len(config.BackOff) == 0 && config.InactiveThreshold == 0 &&
		config.DeliverPolicy == jetstream.DeliverAllPolicy && config.ReplayPolicy == jetstream.ReplayInstantPolicy &&
		!config.HeadersOnly && config.RateLimit == 0
}

func (b *Broker) compatibleConsumerPulls(config jetstream.ConsumerConfig) bool {
	return config.MaxAckPending == b.config.MaxAckPending && config.MaxWaiting >= b.config.MaxAckPending &&
		config.MaxRequestBatch >= 0 && config.MaxRequestMaxBytes == 0 &&
		(config.MaxRequestExpires == 0 || config.MaxRequestExpires >= fetchWait)
}

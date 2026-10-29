package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	connectionTimeout      = 5 * time.Second
	connectionCloseTimeout = time.Second
	heartbeatInterval      = 10 * time.Second
	publicationTimeout     = 10 * time.Second
	minRetryDelay          = time.Second
	maxRetryDelay          = 10 * time.Second
	maxPublicationBatch    = 256
	consumerTag            = "forcad-worker"
)

type Broker struct {
	url         string
	queue       string
	concurrency int
	logger      *slog.Logger
	draining    chan struct{}
	drainOnce   sync.Once
	connected   atomic.Bool
}

func New(url, queue string, concurrency int, logger *slog.Logger) *Broker {
	return &Broker{url: url, queue: queue, concurrency: concurrency, logger: logger, draining: make(chan struct{})}
}

func (b *Broker) Drain()          { b.drainOnce.Do(func() { close(b.draining) }) }
func (b *Broker) Connected() bool { return b.connected.Load() }
func closeConnection(conn *amqp.Connection) {
	_ = conn.CloseDeadline(time.Now().Add(connectionCloseTimeout))
}

func (b *Broker) dial(ctx context.Context) (*amqp.Connection, error) {
	const location = "rabbitmq.Broker.dial"
	dialer := net.Dialer{Timeout: connectionTimeout}
	conn, err := amqp.DialConfig(b.url, amqp.Config{Heartbeat: heartbeatInterval, Locale: "en_US", Dial: func(network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, fmt.Errorf("%s - open broker network connection: %w", location, err)
		}
		if err = conn.SetDeadline(time.Now().Add(connectionTimeout)); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%s - set broker connection deadline: %w", location, err)
		}
		return conn, nil
	}})
	if err != nil {
		return nil, fmt.Errorf("%s - establish broker connection: %w", location, err)
	}
	return conn, nil
}

func (b *Broker) declare(ch *amqp.Channel) error {
	const location = "rabbitmq.Broker.declare"
	if _, err := ch.QueueDeclare(b.queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("%s - declare checker queue: %w", location, err)
	}
	return nil
}

// Publish returns the IDs confirmed by RabbitMQ, including partial success.
// The caller checkpoints only these IDs in the durable PostgreSQL outbox.
// A crash before checkpointing may redeliver a job; the worker's SQL claim
// prevents a second checker process from executing it.
func (b *Broker) Publish(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	const location = "rabbitmq.Broker.Publish"
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxPublicationBatch {
		return nil, fmt.Errorf("%s - publication batch exceeds 256 jobs", location)
	}
	for _, id := range ids {
		if id == uuid.Nil() {
			return nil, fmt.Errorf("%s - checker job ID must not be nil", location)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, publicationTimeout)
	defer cancel()
	conn, err := b.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - connect checker broker: %w", location, err)
	}
	defer closeConnection(conn)
	stop := context.AfterFunc(ctx, func() { closeConnection(conn) })
	defer stop()
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("%s - open checker channel: %w", location, err)
	}
	defer func() { _ = ch.Close() }()
	if err = b.declare(ch); err != nil {
		return nil, fmt.Errorf("%s - prepare checker queue: %w", location, err)
	}
	if err = ch.Confirm(false); err != nil {
		return nil, fmt.Errorf("%s - enable publisher confirms: %w", location, err)
	}
	return publishConfirmedBatch(ctx, ids, func(ctx context.Context, id uuid.UUID) (publicationConfirmation, error) {
		messageID := id.String()
		confirmation, err := ch.PublishWithDeferredConfirmWithContext(ctx, "", b.queue, false, false, amqp.Publishing{
			ContentType: "text/plain", DeliveryMode: amqp.Persistent, MessageId: messageID, Body: []byte(messageID),
		})
		if err != nil {
			return nil, err
		}
		return confirmation, nil
	})
}

type publicationConfirmation interface {
	WaitContext(ctx context.Context) (bool, error)
	Done() <-chan struct{}
	Acked() bool
}

type pendingPublication struct {
	id           uuid.UUID
	confirmation publicationConfirmation
}

func publishConfirmedBatch(ctx context.Context, ids []uuid.UUID, publish func(context.Context, uuid.UUID) (publicationConfirmation, error)) ([]uuid.UUID, error) {
	const location = "rabbitmq.publishConfirmedBatch"
	pending := make([]pendingPublication, 0, len(ids))
	var publishErr error
	for _, id := range ids {
		confirmation, err := publish(ctx, id)
		if err != nil {
			publishErr = fmt.Errorf("%s - publish checker job %q: %w", location, id, err)
			break
		}
		pending = append(pending, pendingPublication{id: id, confirmation: confirmation})
	}

	confirmed := make([]uuid.UUID, 0, len(pending))
	unconfirmed := 0
	var waitErr error
	for _, publication := range pending {
		acked, err := waitForConfirmation(ctx, publication.confirmation)
		if acked && err == nil {
			confirmed = append(confirmed, publication.id)
			continue
		}
		unconfirmed++
		if waitErr == nil && err != nil {
			waitErr = err
		}
	}
	if unconfirmed > 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			waitErr = errors.Join(waitErr, ctxErr)
		}
		if waitErr != nil {
			waitErr = fmt.Errorf("%s - %d checker job publications were not confirmed: %w", location, unconfirmed, waitErr)
		} else {
			waitErr = fmt.Errorf("%s - %d checker job publications were not confirmed", location, unconfirmed)
		}
	}
	return confirmed, errors.Join(publishErr, waitErr)
}

func waitForConfirmation(ctx context.Context, confirmation publicationConfirmation) (bool, error) {
	acked, err := confirmation.WaitContext(ctx)
	if err == nil {
		return acked, nil
	}
	select {
	case <-confirmation.Done():
		return confirmation.Acked(), nil
	default:
		return false, err
	}
}

type Handler func(ctx context.Context, jobID uuid.UUID) error

// Run reconnects on broker failure, with prefetch equal to bounded worker
// concurrency. Acknowledgement happens only after the handler settles in SQL.
func (b *Broker) Run(ctx context.Context, handle Handler) error {
	const location = "rabbitmq.Broker.Run"
	if b.concurrency < 1 {
		return fmt.Errorf("%s - worker concurrency must be positive", location)
	}
	backoff := minRetryDelay
	for ctx.Err() == nil {
		if b.isDraining() {
			return nil
		}
		err := b.consume(ctx, handle)
		if b.isDraining() {
			return nil
		}
		if ctx.Err() != nil {
			return nil //nolint:nilerr // Cancellation ends the consumer normally; unfinished jobs remain recoverable.
		}
		b.logger.Warn("checker broker disconnected", "error", err, "retry_in", backoff)
		if !sleep(ctx, backoff) {
			break
		}
		backoff = min(maxRetryDelay, backoff*2)
	}
	return nil
}

func (b *Broker) consume(ctx context.Context, handle Handler) error {
	const location = "rabbitmq.Broker.consume"
	conn, err := b.dial(ctx)
	if err != nil {
		return fmt.Errorf("%s - connect checker consumer: %w", location, err)
	}
	defer closeConnection(conn)
	stop := context.AfterFunc(ctx, func() { closeConnection(conn) })
	defer stop()
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("%s - open checker channel: %w", location, err)
	}
	defer func() { _ = ch.Close() }()
	if err = b.declare(ch); err != nil {
		return fmt.Errorf("%s - prepare checker queue: %w", location, err)
	}
	if err = ch.Qos(b.concurrency, 0, false); err != nil {
		return fmt.Errorf("%s - configure consumer prefetch: %w", location, err)
	}
	deliveries, err := ch.Consume(b.queue, consumerTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("%s - start checker consumer: %w", location, err)
	}
	b.connected.Store(true)
	defer b.connected.Store(false)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	var workers sync.WaitGroup
	for range b.concurrency {
		workers.Go(func() { b.consumeDeliveries(runCtx, deliveries, handle, cancel) })
	}

	var closeErr *amqp.Error
	select {
	case <-runCtx.Done():
	case closeErr = <-closed:
	case <-b.draining:
		// Cancel intake, but keep current handlers and their acknowledgement
		// channel alive until the application shutdown deadline.
		_ = ch.Cancel(consumerTag, false)
		workers.Wait()
		return nil
	}
	cancel()
	closeConnection(conn)
	workers.Wait()
	if closeErr != nil {
		return fmt.Errorf("%s - checker consumer connection closed: %w", location, closeErr)
	}
	return fmt.Errorf("%s - checker consumer connection closed", location)
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func parseJobID(value string) (uuid.UUID, error) {
	const location = "rabbitmq.parseJobID"
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil(), fmt.Errorf("%s - parse checker job UUID: %w", location, err)
	}
	if id == uuid.Nil() || id.String() != value {
		return uuid.Nil(), fmt.Errorf("%s - checker job UUID must be canonical and non-nil", location)
	}
	return id, nil
}

func (b *Broker) isDraining() bool {
	select {
	case <-b.draining:
		return true
	default:
		return false
	}
}

func (b *Broker) consumeDeliveries(ctx context.Context, deliveries <-chan amqp.Delivery, handle Handler, cancel context.CancelFunc) {
	for {
		if b.isDraining() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case delivery, open := <-deliveries:
			if !open {
				return
			}
			if b.isDraining() {
				_ = delivery.Nack(false, true)
				return
			}
			if err := b.settleDelivery(ctx, delivery, handle); err != nil {
				cancel()
				return
			}
		}
	}
}

func (b *Broker) settleDelivery(ctx context.Context, delivery amqp.Delivery, handle Handler) error {
	const location = "rabbitmq.Broker.settleDelivery"
	id, err := parseJobID(string(delivery.Body))
	if err != nil {
		_ = delivery.Reject(false)
		return nil //nolint:nilerr // Invalid IDs are rejected without requeue, so discarding the delivery succeeds.
	}
	if err := handle(ctx, id); err != nil {
		b.logger.WarnContext(ctx, "checker job not settled", "job_id", id.String(), "error", err)
		_ = sleep(ctx, minRetryDelay)
		if err := delivery.Nack(false, true); err != nil {
			return fmt.Errorf("%s - requeue checker job: %w", location, err)
		}
		return nil
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("%s - acknowledge checker job: %w", location, err)
	}
	return nil
}

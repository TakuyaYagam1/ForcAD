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
	consumerRecoveryGrace  = 30 * time.Second
	stableSessionPeriod    = time.Minute
)

var errConsumerHandlersStuck = errors.New("checker handlers did not stop after cancellation")

type Broker struct {
	url           string
	queue         string
	concurrency   int
	logger        *slog.Logger
	draining      chan struct{}
	drainOnce     sync.Once
	connected     atomic.Bool
	sessionStable atomic.Bool
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
	return b.run(ctx, handle, b.consume)
}

func (b *Broker) run(ctx context.Context, handle Handler, consume func(context.Context, Handler) error) error {
	const location = "rabbitmq.Broker.Run"
	if b.concurrency < 1 {
		return fmt.Errorf("%s - worker concurrency must be positive", location)
	}
	backoff := minRetryDelay
	for ctx.Err() == nil {
		if b.isDraining() {
			return nil
		}
		b.sessionStable.Store(false)
		err := consume(ctx, handle)
		if errors.Is(err, errConsumerHandlersStuck) {
			return fmt.Errorf("%s - replace stuck consumer: %w", location, err)
		}
		if b.isDraining() {
			return nil
		}
		if ctx.Err() != nil {
			return nil //nolint:nilerr // Cancellation ends the consumer normally; unfinished jobs remain recoverable.
		}
		if b.sessionStable.Load() {
			backoff = minRetryDelay
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
	notifications := consumerNotifications{
		connectionClosed: conn.NotifyClose(make(chan *amqp.Error, 1)),
		channelClosed:    ch.NotifyClose(make(chan *amqp.Error, 1)),
		canceled:         ch.NotifyCancel(make(chan string, 1)),
	}
	deliveries, err := ch.Consume(b.queue, consumerTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("%s - start checker consumer: %w", location, err)
	}
	return b.consumeSession(ctx, deliveries, handle, notifications,
		func() error { return ch.Cancel(consumerTag, false) },
		func() { closeConnection(conn) })
}

type consumerNotifications struct {
	connectionClosed <-chan *amqp.Error
	channelClosed    <-chan *amqp.Error
	canceled         <-chan string
}

// consumeSession supervises intake independently of the connection: RabbitMQ
// can cancel a consumer or close its channel while keeping the connection open.
func (b *Broker) consumeSession(ctx context.Context, deliveries <-chan amqp.Delivery, handle Handler,
	notifications consumerNotifications, stopIntake func() error, closeTransport func(),
) error {
	const location = "rabbitmq.Broker.consumeSession"
	b.connected.Store(true)
	defer b.connected.Store(false)
	started := time.Now()
	session := b.startSessionWorkers(ctx, deliveries, handle)
	defer session.cancelWork(context.Canceled)
	defer session.stopIntake()

	var sessionErr error
	select {
	case <-ctx.Done():
		sessionErr = context.Cause(ctx)
	case sessionErr = <-session.failures:
	case err := <-notifications.connectionClosed:
		sessionErr = consumerClosedError("connection", err)
	case err := <-notifications.channelClosed:
		sessionErr = consumerClosedError("channel", err)
	case tag := <-notifications.canceled:
		sessionErr = fmt.Errorf("checker consumer canceled by broker: %q", tag)
	case <-b.draining:
		// Cancel intake, but keep current handlers and their acknowledgement
		// channel alive until the application shutdown deadline.
		b.connected.Store(false)
		session.stopIntake()
		sessionErr = cancelConsumer(ctx, stopIntake)
		if sessionErr == nil {
			if err := session.finish(ctx, 0); err != nil {
				return fmt.Errorf("%s - drain checker consumer: %w", location, err)
			}
			return nil
		}
	}
	b.sessionStable.Store(time.Since(started) >= stableSessionPeriod)
	b.connected.Store(false)
	session.stopIntake()
	closeTransport()
	// Loss of the acknowledgement transport does not imply loss of the SQL
	// claim. Let already-started handlers settle durably before reconnecting;
	// duplicates will then observe the closed job. Lease loss still cancels
	// work inside the handler, and this recovery window is strictly bounded.
	drainErr := session.finish(ctx, consumerRecoveryGrace)
	return fmt.Errorf("%s - stop checker consumer: %w", location, errors.Join(sessionErr, drainErr))
}

type consumerSession struct {
	stopIntake context.CancelFunc
	cancelWork context.CancelCauseFunc
	done       <-chan struct{}
	failures   <-chan error
}

func (b *Broker) startSessionWorkers(ctx context.Context, deliveries <-chan amqp.Delivery, handle Handler) consumerSession {
	workCtx, cancelWork := context.WithCancelCause(ctx)
	intakeCtx, stopIntake := context.WithCancel(ctx)
	failures := make(chan error, b.concurrency)
	done := make(chan struct{})
	var workers sync.WaitGroup
	for range b.concurrency {
		workers.Go(func() {
			if err := b.consumeDeliveries(workCtx, intakeCtx, deliveries, handle); err != nil {
				failures <- err
			}
		})
	}
	go func() { workers.Wait(); close(done) }()
	return consumerSession{stopIntake: stopIntake, cancelWork: cancelWork, done: done, failures: failures}
}

// A zero grace uses the application shutdown context for a deliberate drain.
func (session consumerSession) finish(ctx context.Context, grace time.Duration) error {
	var timeout <-chan time.Time
	if grace > 0 {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		timeout = timer.C
	}
	var cause error
	select {
	case <-session.done:
		return nil
	case <-ctx.Done():
		cause = context.Cause(ctx)
	case <-timeout:
		cause = context.DeadlineExceeded
	}
	session.cancelWork(cause)
	timer := time.NewTimer(connectionTimeout)
	defer timer.Stop()
	select {
	case <-session.done:
		return cause
	case <-timer.C:
		return errors.Join(cause, errConsumerHandlersStuck)
	}
}

func consumerClosedError(kind string, err *amqp.Error) error {
	if err != nil {
		return fmt.Errorf("checker consumer %s closed: %w", kind, err)
	}
	return fmt.Errorf("checker consumer %s closed", kind)
}

func cancelConsumer(ctx context.Context, stopIntake func() error) error {
	const location = "rabbitmq.cancelConsumer"
	done := make(chan error, 1)
	go func() { done <- stopIntake() }()
	timer := time.NewTimer(connectionTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s - cancel checker intake: %w", location, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s - cancel checker intake: %w", location, context.Cause(ctx))
	case <-timer.C:
		return fmt.Errorf("%s - cancel checker intake: %w", location, context.DeadlineExceeded)
	}
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

func (b *Broker) consumeDeliveries(ctx, intakeCtx context.Context, deliveries <-chan amqp.Delivery, handle Handler) error {
	for {
		if b.isDraining() || intakeCtx.Err() != nil {
			return nil //nolint:nilerr // Ending intake is deliberate; existing handler settlement is supervised separately.
		}
		select {
		case <-ctx.Done():
			return nil
		case <-intakeCtx.Done():
			return nil
		case <-b.draining:
			return nil
		case delivery, open := <-deliveries:
			if !open {
				if b.isDraining() || ctx.Err() != nil {
					return nil //nolint:nilerr // Cancellation or local drain intentionally ends intake without a delivery failure.
				}
				return errors.New("checker consumer deliveries closed unexpectedly")
			}
			if b.isDraining() || intakeCtx.Err() != nil {
				if err := delivery.Nack(false, true); err != nil {
					return fmt.Errorf("rabbitmq.Broker.consumeDeliveries - requeue during drain: %w", err)
				}
				return nil
			}
			if err := b.settleDelivery(ctx, delivery, handle); err != nil {
				return err
			}
		}
	}
}

func (b *Broker) settleDelivery(ctx context.Context, delivery amqp.Delivery, handle Handler) error {
	const location = "rabbitmq.Broker.settleDelivery"
	id, err := parseJobID(string(delivery.Body))
	if err != nil {
		if err := delivery.Reject(false); err != nil {
			return fmt.Errorf("%s - reject invalid checker job: %w", location, err)
		}
		return nil // Invalid IDs were successfully rejected without requeue.
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

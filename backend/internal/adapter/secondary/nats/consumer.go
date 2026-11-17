package natsbroker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	"github.com/nats-io/nats.go/jetstream"
)

var errConsumerHandlersStuck = errors.New("checker handlers did not stop after cancellation")

type Handler func(ctx context.Context, jobID uuid.UUID) error
type pullConsumer interface {
	Fetch(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error)
}

// Run binds to the shared durable consumer. It never creates or updates it.
func (b *Broker) Run(ctx context.Context, handle Handler) error {
	const location = "natsbroker.Broker.Run"
	if err := b.validate(); err != nil {
		return fmt.Errorf("%s - validate configuration: %w", location, err)
	}
	if handle == nil {
		return fmt.Errorf("%s - checker handler is required", location)
	}
	b.connected.Store(false)
	backoff := minRetryDelay
	for !b.stopping(ctx) {
		started := time.Now()
		err := b.consume(ctx, handle)
		if errors.Is(err, errConsumerHandlersStuck) {
			return fmt.Errorf("%s - replace stuck consumer: %w", location, err)
		}
		if b.stopping(ctx) {
			break
		}
		if time.Since(started) >= time.Minute {
			backoff = minRetryDelay
		}
		b.logger.WarnContext(ctx, "checker broker disconnected", "error", err, "retry_in", backoff)
		if !b.wait(ctx, backoff) {
			return nil
		}
		backoff = min(maxRetryDelay, 2*backoff)
	}
	return nil
}

// Cancellation and an operator-requested drain are normal supervisor stops;
// the transport error from the ending session is relevant only for retries.
func (b *Broker) stopping(ctx context.Context) bool {
	return ctx.Err() != nil || b.isDraining()
}

func (b *Broker) consume(ctx context.Context, handle Handler) error {
	const location = "natsbroker.Broker.consume"
	bindCtx, cancel := context.WithTimeout(ctx, connectionTimeout)
	c, err := b.connection(bindCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("%s - connect checker consumer: %w", location, err)
	}
	consumer, err := c.js.Consumer(bindCtx, b.config.Stream, b.config.Consumer)
	if err != nil {
		cancel()
		return fmt.Errorf("%s - bind checker consumer: %w", location, err)
	}
	info, err := consumer.Info(bindCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("%s - inspect checker consumer: %w", location, err)
	}
	if err := b.validateConsumer(info.Config); err != nil {
		return fmt.Errorf("%s - validate checker consumer: %w", location, err)
	}
	if err := b.consumeSession(ctx, consumer, handle, max(time.Millisecond, min(10*time.Second, info.Config.AckWait/3))); err != nil {
		return fmt.Errorf("%s - consume checker jobs: %w", location, err)
	}
	return nil
}

func (b *Broker) consumeSession(
	ctx context.Context,
	consumer pullConsumer,
	handle Handler,
	progressInterval time.Duration,
) error {
	const location = "natsbroker.Broker.consumeSession"
	b.connected.Store(true)
	defer b.connected.Store(false)
	session := b.startSessionWorkers(ctx, consumer, handle, progressInterval)
	defer session.cancelWork(context.Canceled)
	defer session.stopIntake()
	var cause error
	select {
	case <-ctx.Done():
		cause = context.Cause(ctx)
	case cause = <-session.failures:
	case <-b.draining:
		b.connected.Store(false)
		session.stopIntake()
		if err := session.finish(ctx, 0); err != nil {
			return fmt.Errorf("%s - drain checker handlers: %w", location, err)
		}
		return nil
	}
	b.connected.Store(false)
	session.stopIntake()
	// A broker error does not invalidate the independent SQL lease. Existing
	// work gets a bounded chance to settle before another session is opened.
	if err := errors.Join(cause, session.finish(ctx, consumerRecoveryGrace)); err != nil {
		return fmt.Errorf("%s - recover checker handlers: %w", location, err)
	}
	return nil
}

type consumerSession struct {
	stopIntake context.CancelFunc
	cancelWork context.CancelCauseFunc
	done       <-chan struct{}
	failures   <-chan error
}

func (b *Broker) startSessionWorkers(
	ctx context.Context,
	consumer pullConsumer,
	handle Handler,
	interval time.Duration,
) consumerSession {
	const location = "natsbroker.Broker.startSessionWorkers"
	workCtx, cancelWork := context.WithCancelCause(ctx)
	intakeCtx, stopIntake := context.WithCancel(ctx)
	done := make(chan struct{})
	failures := make(chan error, b.config.Concurrency)
	var workers sync.WaitGroup
	for range b.config.Concurrency {
		workers.Go(func() {
			if err := b.consumeDeliveries(workCtx, intakeCtx, consumer, handle, interval); err != nil {
				failures <- fmt.Errorf("%s - consume checker deliveries: %w", location, err)
			}
		})
	}
	go func() {
		workers.Wait()
		close(done)
	}()
	return consumerSession{
		stopIntake: stopIntake,
		cancelWork: cancelWork,
		done:       done,
		failures:   failures,
	}
}

func (session consumerSession) finish(ctx context.Context, grace time.Duration) error {
	const location = "natsbroker.consumerSession.finish"
	var deadline <-chan time.Time
	if grace > 0 {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		deadline = timer.C
	}
	var cause error
	select {
	case <-session.done:
		return nil
	case <-ctx.Done():
		cause = context.Cause(ctx)
	case <-deadline:
		cause = context.DeadlineExceeded
	}
	session.cancelWork(cause)
	timer := time.NewTimer(connectionTimeout)
	defer timer.Stop()
	select {
	case <-session.done:
		return fmt.Errorf("%s - stop checker handlers: %w", location, cause)
	case <-timer.C:
		return fmt.Errorf("%s - wait for canceled checker handlers: %w", location, errors.Join(cause, errConsumerHandlersStuck))
	}
}

func (b *Broker) wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	case <-b.draining:
		return false
	}
}

package natsbroker

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/nats-io/nats.go/jetstream"
)

type delivery interface {
	Data() []byte
	DoubleAck(ctx context.Context) error
	NakWithDelay(delay time.Duration) error
	InProgress() error
	Term() error
}

func (b *Broker) consumeDeliveries(
	ctx, intakeCtx context.Context,
	consumer pullConsumer,
	handle Handler,
	interval time.Duration,
) error {
	const location = "natsbroker.Broker.consumeDeliveries"
	for intakeCtx.Err() == nil && ctx.Err() == nil && !b.isDraining() {
		// A slot requests one message only while free. There is no local buffer
		// of reserved jobs behind a busy checker or across its execution time.
		batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(fetchWait))
		if err != nil {
			return fmt.Errorf("%s - fetch checker job: %w", location, err)
		}
		b.connected.Store(true)
		for msg := range batch.Messages() {
			if intakeCtx.Err() != nil || ctx.Err() != nil || b.isDraining() {
				if err := msg.NakWithDelay(minRetryDelay); err != nil {
					return fmt.Errorf("%s - return job during drain: %w", location, err)
				}
				return nil
			}
			b.connected.Store(true)
			if err := b.settleDelivery(ctx, msg, handle, interval); err != nil {
				return fmt.Errorf("%s - settle checker job: %w", location, err)
			}
		}
		if err := batch.Error(); err != nil {
			return fmt.Errorf("%s - receive checker job: %w", location, err)
		}
	}
	return nil
}

func (b *Broker) settleDelivery(
	ctx context.Context,
	msg delivery,
	handle Handler,
	interval time.Duration,
) error {
	const location = "natsbroker.Broker.settleDelivery"
	id, err := parseJobID(string(msg.Data()))
	if err != nil {
		if err := msg.Term(); err != nil {
			return fmt.Errorf("%s - terminate invalid checker job: %w", location, err)
		}
		return nil
	}
	progressCtx, stopProgress := context.WithCancel(ctx)
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-progressCtx.Done():
				return
			case <-ticker.C:
				// Keep the independent SQL lease alive through transient transport
				// failure. A missing broker ack can safely cause a later duplicate.
				if err := msg.InProgress(); err != nil {
					b.logger.DebugContext(ctx, "checker progress acknowledgement unavailable", "error", err)
				}
			}
		}
	}()
	handleErr := handle(ctx, id)
	stopProgress()
	<-progressDone
	if handleErr != nil {
		b.logger.WarnContext(ctx, "checker job not settled", "job_id", id.String(), "error", handleErr)
		if err := msg.NakWithDelay(minRetryDelay); err != nil {
			return fmt.Errorf("%s - requeue checker job: %w", location, err)
		}
		return nil
	}
	// The handler's nil result means SQL settlement is durable, even if the
	// request context expired just as completion was confirmed.
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
	defer cancel()
	if err := msg.DoubleAck(ackCtx); err != nil {
		return fmt.Errorf("%s - acknowledge settled checker job: %w", location, err)
	}
	return nil
}

func parseJobID(value string) (uuid.UUID, error) {
	const location = "natsbroker.parseJobID"
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil(), fmt.Errorf("%s - parse checker job UUID: %w", location, err)
	}
	if id == uuid.Nil() || id.String() != value {
		return uuid.Nil(), fmt.Errorf("%s - checker job UUID must be canonical and non-nil", location)
	}
	return id, nil
}

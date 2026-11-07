package natsbroker

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/nats-io/nats.go/jetstream"
)

type pendingPublication struct {
	id     uuid.UUID
	future jetstream.PubAckFuture
}

// Publish returns only broker-confirmed IDs. It deliberately does not set
// Nats-Msg-Id: recovering an SQL lease may require republishing the same job ID
// after its previous delivery was acknowledged. SQL claims deduplicate work.
func (b *Broker) Publish(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	const location = "natsbroker.Broker.Publish"
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
	// Serialize batches so timeout cleanup cannot invalidate another batch.
	select {
	case b.publicationGate <- struct{}{}:
		defer func() {
			<-b.publicationGate
		}()
	case <-ctx.Done():
		return nil, fmt.Errorf("%s - wait for publisher: %w", location, ctx.Err())
	}
	if err := b.Ensure(ctx); err != nil {
		return nil, fmt.Errorf("%s - prepare transport: %w", location, err)
	}
	c, err := b.connection(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - get publisher: %w", location, err)
	}
	confirmed, err := publishConfirmedBatch(ctx, ids, b.config.Stream, func(id uuid.UUID) (jetstream.PubAckFuture, error) {
		return b.publishJob(ctx, c, id)
	})
	if err != nil {
		c.js.CleanupPublisher()
		return confirmed, fmt.Errorf("%s - publish checker jobs: %w", location, err)
	}
	return confirmed, nil
}

func (b *Broker) publishJob(ctx context.Context, c *client, id uuid.UUID) (jetstream.PubAckFuture, error) {
	const location = "natsbroker.Broker.publishJob"
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s - check publication context: %w", location, err)
	}
	future, err := c.js.PublishAsync(b.config.Subject, []byte(id.String()))
	if err != nil {
		return nil, fmt.Errorf("%s - publish checker job: %w", location, err)
	}
	return future, nil
}

func publishConfirmedBatch(
	ctx context.Context,
	ids []uuid.UUID,
	stream string,
	publish func(uuid.UUID) (jetstream.PubAckFuture, error),
) ([]uuid.UUID, error) {
	const location = "natsbroker.publishConfirmedBatch"
	pending := make([]pendingPublication, 0, len(ids))
	var publishErr error
	for _, id := range ids {
		future, err := publish(id)
		if err != nil {
			publishErr = fmt.Errorf("%s - publish job %s: %w", location, id, err)
			break
		}
		pending = append(pending, pendingPublication{
			id:     id,
			future: future,
		})
	}
	confirmed := make([]uuid.UUID, 0, len(pending))
	var ackErr error
	for _, item := range pending {
		ack, err := waitForConfirmation(ctx, item.future)
		if err == nil && (ack == nil || ack.Stream != stream || ack.Sequence == 0) {
			err = fmt.Errorf("%s - invalid JetStream publication acknowledgement", location)
		}
		if err != nil {
			ackErr = errors.Join(ackErr, fmt.Errorf("%s - confirm job %s: %w", location, item.id, err))
			continue
		}
		confirmed = append(confirmed, item.id)
	}
	if err := errors.Join(publishErr, ackErr); err != nil {
		return confirmed, fmt.Errorf("%s - confirm publication batch: %w", location, err)
	}
	return confirmed, nil
}

func waitForConfirmation(ctx context.Context, future jetstream.PubAckFuture) (*jetstream.PubAck, error) {
	const location = "natsbroker.waitForConfirmation"
	select {
	case ack := <-future.Ok():
		return ack, nil
	case err := <-future.Err():
		if err != nil {
			return nil, fmt.Errorf("%s - receive publication acknowledgement: %w", location, err)
		}
		return nil, nil
	case <-ctx.Done():
		// A confirmation may have arrived concurrently with cancellation.
		select {
		case ack := <-future.Ok():
			return ack, nil
		default:
			return nil, fmt.Errorf("%s - wait for publication acknowledgement: %w", location, ctx.Err())
		}
	}
}

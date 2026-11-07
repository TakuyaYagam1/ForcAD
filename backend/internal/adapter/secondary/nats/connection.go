package natsbroker

import (
	"context"
	"fmt"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type client struct {
	conn *nats.Conn
	js   jetstream.JetStream
}

func (b *Broker) transportConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.closed && b.client != nil && b.client.conn.IsConnected()
}

func (b *Broker) connection(ctx context.Context) (*client, error) {
	const location = "natsbroker.Broker.connection"
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s - check connection context: %w", location, err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, fmt.Errorf("%s - checker broker is closed", location)
	}
	if b.client != nil && !b.client.conn.IsClosed() {
		return b.client, nil
	}
	timeout := connectionTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("%s - check connection deadline: %w", location, context.DeadlineExceeded)
	}
	conn, err := nats.Connect(b.config.URL, nats.Name("forcad-checker"), nats.Timeout(timeout),
		nats.MaxReconnects(-1), nats.ReconnectWait(minRetryDelay), nats.ReconnectBufSize(0),
		nats.PingInterval(10*time.Second), nats.MaxPingsOutstanding(2),
		nats.DisconnectErrHandler(func(_ *nats.Conn, _ error) {
			b.connected.Store(false)
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			b.connected.Store(false)
		}))
	if err != nil {
		return nil, fmt.Errorf("%s - connect to NATS: %w", location, err)
	}
	js, err := jetstream.New(conn, jetstream.WithPublishAsyncMaxPending(maxPublicationBatch))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s - create JetStream client: %w", location, err)
	}
	b.client = &client{
		conn: conn,
		js:   js,
	}
	return b.client, nil
}

// Close releases the lazy connection. For a graceful shutdown, first call
// Drain and wait for Run to settle its handlers, then Close.
func (b *Broker) Close() error {
	const location = "natsbroker.Broker.Close"
	b.Drain()
	b.mu.Lock()
	b.closed = true
	c := b.client
	b.mu.Unlock()
	if c == nil {
		return nil
	}
	var err error
	if c.conn.IsConnected() {
		err = c.conn.FlushTimeout(time.Second)
	}
	c.js.CleanupPublisher()
	c.conn.Close()
	if err != nil {
		return fmt.Errorf("%s - flush connection: %w", location, err)
	}
	return nil
}

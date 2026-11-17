package websocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	ws "github.com/coder/websocket"
	wskit "github.com/wahrwelt-kit/go-wskit"
)

const (
	clientWriteWait    = 3 * time.Second
	clientPingInterval = 20 * time.Second
	clientReadLimit    = 1024
)

// eventClient keeps ForcAD's bounded outbox policy local while the existing hub
// retains registration, fan-out and Redis publication behavior.
type eventClient struct {
	hub           *wskit.Hub
	conn          *ws.Conn
	outbox        *eventOutbox
	done          chan struct{}
	closeOnce     sync.Once
	connCloseOnce sync.Once
	onEvicted     func()
}

var _ wskit.Subscriber = (*eventClient)(nil)

func (e *Events) acceptClient(w http.ResponseWriter, r *http.Request) (*eventClient, error) {
	const location = "websocket.Events.acceptClient"
	// nil preserves coder/websocket's existing same-origin upgrade checks.
	conn, err := ws.Accept(w, r, nil)
	if err != nil {
		return nil, fmt.Errorf("%s - upgrade event connection: %w", location, err)
	}
	conn.SetReadLimit(clientReadLimit)
	client := &eventClient{
		hub:    e.hub,
		conn:   conn,
		outbox: newEventOutbox(clientSendBuffer),
		done:   make(chan struct{}),
		onEvicted: func() {
			e.notificationsDropped.Add(1)
		},
	}
	if err := e.hub.Register(client); err != nil {
		_ = conn.Close(ws.StatusTryAgainLater, err.Error())
		return nil, fmt.Errorf("%s - register event client: %w", location, err)
	}
	return client, nil
}

func (c *eventClient) Send(data []byte) bool {
	accepted, evicted := c.outbox.send(data)
	if evicted && c.onEvicted != nil {
		c.onEvicted()
	}
	return accepted
}

func (c *eventClient) Close() {
	c.closeOnce.Do(func() {
		c.outbox.close()
		close(c.done)
	})
}

func (c *eventClient) closeConn() {
	c.connCloseOnce.Do(func() {
		_ = c.conn.Close(ws.StatusNormalClosure, "")
	})
}

func (c *eventClient) ReadPump(ctx context.Context) {
	defer func() {
		c.Close()
		c.closeConn()
		_ = c.unregister()
	}()
	for {
		if _, _, err := c.conn.Read(ctx); err != nil {
			return
		}
	}
}

// unregister keeps cleanup in the existing HTTP handler, retaining its slot
// until removal is queued or the hub stops. A timeout means nothing was queued.
// Request cancellation or a total deadline cannot abandon cleanup without
// leaving a closed subscriber in the hub. Each attempt uses the hub's bounded
// channel timeout; hub shutdown interrupts the wait.
func (c *eventClient) unregister() error {
	const location = "websocket.eventClient.unregister"
	for {
		err := c.hub.Unregister(c)
		if err == nil {
			return nil
		}
		if !errors.Is(err, wskit.ErrOperationTimeout) {
			return fmt.Errorf("%s - remove event client: %w", location, err)
		}
	}
}

func (c *eventClient) write(ctx context.Context, message []byte) error {
	const location = "websocket.eventClient.write"
	ctx, cancel := context.WithTimeout(ctx, clientWriteWait)
	defer cancel()
	if err := c.conn.Write(ctx, ws.MessageText, message); err != nil {
		return fmt.Errorf("%s - write event message: %w", location, err)
	}
	return nil
}

func (c *eventClient) ping(ctx context.Context) error {
	const location = "websocket.eventClient.ping"
	ctx, cancel := context.WithTimeout(ctx, clientWriteWait)
	defer cancel()
	if err := c.conn.Ping(ctx); err != nil {
		return fmt.Errorf("%s - ping event connection: %w", location, err)
	}
	return nil
}

func (c *eventClient) WritePump(ctx context.Context) {
	ticker := time.NewTicker(clientPingInterval)
	defer func() {
		ticker.Stop()
		c.closeConn()
	}()
	for {
		select {
		case <-c.outbox.ready:
			if message, ok := c.outbox.take(); ok {
				if err := c.write(ctx, message); err != nil {
					return
				}
			}
		case <-ticker.C:
			if err := c.ping(ctx); err != nil {
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

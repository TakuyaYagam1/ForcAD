package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	wskit "github.com/wahrwelt-kit/go-wskit"
)

// Keep each client's FIFO bounded. Disposable notifications yield queued
// capacity to state events when full, preserving the surviving event order.
const clientSendBuffer = 256

type Observer interface {
	WebSocketOpened(ctx context.Context) func()
}

// Events distributes public projections only. Database commits never depend on
// delivery to connected browsers; reconnects fetch a fresh REST snapshot.
type Events struct {
	hub                  *wskit.Hub
	client               *redis.Client
	logger               *slog.Logger
	slots                chan struct{}
	dropped              atomic.Uint64
	notificationsDropped atomic.Uint64
	observer             Observer
}

// SetObserver attaches process metrics before any connection is accepted.
func (e *Events) SetObserver(observer Observer) {
	e.observer = observer
}

func New(client *redis.Client, logger *slog.Logger) *Events {
	events := &Events{
		client: client,
		logger: logger,
		slots:  make(chan struct{}, 2048),
	}
	hub := wskit.NewHub(
		wskit.WithBroadcastBuf(128), wskit.WithRegisterBuf(64),
		wskit.WithChannelTimeout(time.Second),
		wskit.WithOnDrop(events.drop),
		wskit.WithOnError(func(op string, err error) {
			logger.Warn("event transport error", "operation", op, "error", err)
		}),
	)
	events.hub = hub
	return events
}

func (e *Events) Connections() int {
	return len(e.slots)
}
func (e *Events) ConnectionLimit() int {
	return cap(e.slots)
}
func (e *Events) SlowClientDrops() uint64 {
	return e.dropped.Load()
}
func (e *Events) NotificationDrops() uint64 {
	return e.notificationsDropped.Load()
}

func (e *Events) drop(subscriber wskit.Subscriber, data []byte) {
	if notificationEvent(data) {
		// The activity feed is best effort. Scores come from full snapshots;
		// a notification burst must not disconnect an otherwise healthy reader.
		e.notificationsDropped.Add(1)
		return
	}
	// A missed state update requires reconnecting and fetching a fresh snapshot.
	e.dropped.Add(1)
	subscriber.Close()
}

func (e *Events) Run(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Go(func() {
		e.hub.Run(ctx)
	})
	workers.Go(func() {
		e.subscribe(ctx)
	})
	workers.Wait()
}

func (e *Events) Publish(ctx context.Context, event string, payload any) error {
	const location = "websocket.Events.Publish"
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, err := json.Marshal(wskit.NewEvent(event, payload))
	if err != nil {
		return fmt.Errorf("%s - encode event: %w", location, err)
	}
	if e.client == nil {
		if err := e.hub.Broadcast(data); err != nil {
			return fmt.Errorf("%s - broadcast event locally: %w", location, err)
		}
		return nil
	}
	// Each API replica receives the same Redis publication, including the
	// publishing replica. Receivers need no local subscriber goroutine.
	if err := e.client.Publish(bounded, "forcad:events", data).Err(); err != nil {
		return fmt.Errorf("%s - publish event through Redis: %w", location, err)
	}
	return nil
}

func (e *Events) subscribe(ctx context.Context) {
	if e.client == nil {
		return
	}
	for ctx.Err() == nil {
		pubsub := e.client.Subscribe(ctx, "forcad:events")
		stop := context.AfterFunc(ctx, func() {
			_ = pubsub.Close()
		})
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := pubsub.Receive(probe)
		cancel()
		if err == nil {
			messages := pubsub.Channel(redis.WithChannelSize(128), redis.WithChannelSendTimeout(time.Second))
			for message := range messages {
				if ctx.Err() != nil {
					break
				}
				if err := e.hub.Broadcast([]byte(message.Payload)); err != nil && ctx.Err() == nil {
					e.logger.Warn("event broadcast unavailable", "error", err)
				}
			}
		} else if ctx.Err() == nil {
			e.logger.Warn("event subscription unavailable", "error", err)
		}
		stop()
		_ = pubsub.Close()
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (e *Events) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case e.slots <- struct{}{}:
		defer func() {
			<-e.slots
		}()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "event capacity exceeded", http.StatusServiceUnavailable)
		return
	}
	client, err := e.acceptClient(w, r)
	if err != nil {
		return
	}
	defer client.Close()
	if e.observer != nil {
		closed := e.observer.WebSocketOpened(r.Context())
		defer closed()
	}
	done := make(chan struct{})
	go func(ctx context.Context) {
		defer close(done)
		client.WritePump(ctx)
	}(r.Context())
	client.ReadPump(r.Context())
	client.Close()
	<-done
}

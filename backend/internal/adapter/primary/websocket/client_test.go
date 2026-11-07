package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ws "github.com/coder/websocket"
)

func waitPriorityState(ctx context.Context, t *testing.T, condition func() bool) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("event lifecycle condition did not complete: ", ctx.Err())
		case <-tick.C:
		}
	}
}

type priorityClientObserver struct {
	opened atomic.Int64
	closed atomic.Int64
}

func (o *priorityClientObserver) WebSocketOpened(context.Context) func() {
	o.opened.Add(1)
	return func() {
		o.closed.Add(1)
	}
}

func TestPriorityClientRetainsCrossOriginRejectionAndSlotCleanup(t *testing.T) {
	events := New(nil, slog.New(slog.DiscardHandler))
	observer := &priorityClientObserver{}
	events.SetObserver(observer)
	server := httptest.NewServer(events)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, response, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &ws.DialOptions{
		HTTPHeader: http.Header{
			"Origin": {"https://other.example"},
		},
	})
	if client != nil {
		_ = client.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin handshake accepted: response=%v err=%v", response, err)
	}
	waitPriorityState(ctx, t, func() bool {
		return events.Connections() == 0
	})
	if events.hub.SubscriberCount() != 0 || observer.opened.Load() != 0 || observer.closed.Load() != 0 {
		t.Fatal("a rejected handshake retained a subscriber or observer lifecycle")
	}
}

func TestPriorityClientRetainsSameOriginFramingAndDisconnectLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	events := New(nil, slog.New(slog.DiscardHandler))
	observer := &priorityClientObserver{}
	events.SetObserver(observer)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		events.Run(ctx)
	}()
	server := httptest.NewServer(events)
	defer func() {
		cancel()
		<-runDone
		server.Close()
	}()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &ws.DialOptions{
		HTTPHeader: http.Header{
			"Origin": {server.URL},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	waitSubscriber(t, events)
	for _, name := range []string{"flag_stolen", "init_scoreboard", "admin_message"} {
		if err := events.Publish(ctx, name, map[string]string{
			"value": "unchanged \"bytes\" Ж",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"flag_stolen", "init_scoreboard", "admin_message"} {
		kind, data, err := client.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				Value string `json:"value"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(data, &event); err != nil || kind != ws.MessageText || event.Type != name || event.Payload.Value != "unchanged \"bytes\" Ж" {
			t.Fatalf("framing or payload changed: kind=%v bytes=%q err=%v", kind, data, err)
		}
	}
	if err := client.CloseNow(); err != nil {
		t.Fatal(err)
	}
	waitPriorityState(ctx, t, func() bool {
		return events.Connections() == 0 && events.hub.SubscriberCount() == 0 && observer.closed.Load() == 1
	})
	if observer.opened.Load() != 1 {
		t.Fatal("successful handshake was not observed exactly once")
	}
}

func TestPriorityClientRetainsConnectionCapacity(t *testing.T) {
	events := New(nil, slog.New(slog.DiscardHandler))
	if events.ConnectionLimit() != 2048 || clientSendBuffer != 256 || clientWriteWait != 3*time.Second || clientPingInterval != 20*time.Second || clientReadLimit != 1024 {
		t.Fatal("existing transport limits changed")
	}
	for range events.ConnectionLimit() {
		events.slots <- struct{}{}
	}
	response := httptest.NewRecorder()
	events.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/api/events/ws", nil))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" || events.Connections() != 2048 {
		t.Fatalf("connection capacity response changed: code=%d headers=%v slots=%d", response.Code, response.Header(), events.Connections())
	}
	for range events.ConnectionLimit() {
		<-events.slots
	}
}

func TestPriorityClientRetainsInboundReadLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	events := New(nil, slog.New(slog.DiscardHandler))
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		events.Run(ctx)
	}()
	server := httptest.NewServer(events)
	defer func() {
		cancel()
		<-runDone
		server.Close()
	}()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	waitSubscriber(t, events)
	_ = client.Write(ctx, ws.MessageText, []byte(strings.Repeat("x", clientReadLimit+1)))
	if _, _, err := client.Read(ctx); ws.CloseStatus(err) != ws.StatusMessageTooBig {
		t.Fatalf("oversized inbound message close=%v err=%v", ws.CloseStatus(err), err)
	}
	waitPriorityState(ctx, t, func() bool {
		return events.Connections() == 0 && events.hub.SubscriberCount() == 0
	})
}

func TestPriorityClientServerShutdownWakesIdleWriterAndReleasesLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	hubCtx, stopHub := context.WithCancel(t.Context())
	events := New(nil, slog.New(slog.DiscardHandler))
	observer := &priorityClientObserver{}
	events.SetObserver(observer)
	hubDone := make(chan struct{})
	go func() {
		defer close(hubDone)
		events.Run(hubCtx)
	}()
	server := httptest.NewServer(events)
	defer func() {
		stopHub()
		<-hubDone
		server.Close()
	}()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	waitSubscriber(t, events)
	waitPriorityState(ctx, t, func() bool {
		return events.Connections() == 1 && observer.opened.Load() == 1
	})
	// No data was queued. Hub stop must wake WritePump through client.done.
	stopHub()
	if _, _, err := client.Read(ctx); ws.CloseStatus(err) != ws.StatusNormalClosure {
		t.Fatalf("server shutdown close=%v err=%v", ws.CloseStatus(err), err)
	}
	select {
	case <-hubDone:
	case <-ctx.Done():
		t.Fatal("server hub shutdown did not complete")
	}
	waitPriorityState(ctx, t, func() bool {
		return events.Connections() == 0 && events.hub.SubscriberCount() == 0 && observer.closed.Load() == 1
	})
	if observer.opened.Load() != 1 || events.SlowClientDrops() != 0 || events.NotificationDrops() != 0 {
		t.Fatal("idle shutdown duplicated the lifecycle or reported a queue overflow")
	}
}

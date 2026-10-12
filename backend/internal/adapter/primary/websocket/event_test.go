package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func waitSubscriber(t *testing.T, e *Events) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for e.hub.SubscriberCount() == 0 {
		select {
		case <-deadline.C:
			t.Fatal("WebSocket subscriber did not register")
		case <-tick.C:
		}
	}
}

func TestPublicEventsAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := New(nil, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() { defer close(done); events.Run(ctx) }()
	server := httptest.NewServer(events)
	defer server.Close()
	bounded, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, _, err := websocket.Dial(bounded, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	waitSubscriber(t, events)
	if err := events.Publish(bounded, "flag_stolen", map[string]any{"data": map[string]int{"attacker_id": 1}}); err != nil {
		t.Fatal(err)
	}
	_, data, err := client.Read(bounded)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
		Timestamp time.Time       `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != "flag_stolen" || envelope.Timestamp.IsZero() {
		t.Fatalf("invalid event: %s %v", data, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hub did not stop")
	}
}

func TestPublishPreservesMarshalErrorWithContext(t *testing.T) {
	events := New(nil, slog.New(slog.DiscardHandler))
	err := events.Publish(context.Background(), "invalid", make(chan int))
	var unsupported *json.UnsupportedTypeError
	if !errors.As(err, &unsupported) || !strings.HasPrefix(err.Error(), "websocket.Events.Publish - ") {
		t.Fatalf("Publish() error = %v, want contextual marshal error", err)
	}
}

func TestCrossOriginWebSocketRejected(t *testing.T) {
	events := New(nil, slog.New(slog.DiscardHandler))
	server := httptest.NewServer(events)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://other.example"}}})
	if client != nil {
		_ = client.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross origin accepted: %v %v", response, err)
	}
}

func TestPublicEventBatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	events := New(nil, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() { defer close(done); events.Run(ctx) }()
	defer func() { cancel(); <-done }()
	server := httptest.NewServer(events)
	defer server.Close()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	waitSubscriber(t, events)
	const batchSize = 100
	for batch := range 10 {
		for index := range batchSize {
			if err := events.Publish(ctx, "flag_stolen", map[string]int{"sequence": batch*batchSize + index}); err != nil {
				t.Fatal(err)
			}
		}
		for index := range batchSize {
			_, data, err := client.Read(ctx)
			if err != nil {
				t.Fatalf("ordinary flag batch disconnected reader: %v; drops=%d", err, events.SlowClientDrops())
			}
			var event struct {
				Payload struct {
					Sequence int `json:"sequence"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			if event.Payload.Sequence != batch*batchSize+index {
				t.Fatal("event lost or reordered")
			}
		}
	}
	if events.SlowClientDrops() != 0 {
		t.Fatal("ordinary flag batch treated as a slow consumer")
	}
}

package load_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketReconnectIntervalIsValidatedAndRecorded(t *testing.T) {
	for _, setting := range []struct {
		raw  string
		want float64
	}{
		{
			raw:  "",
			want: 20,
		},
		{
			raw:  "20s",
			want: 20,
		},
		{
			raw:  "0",
			want: 0,
		},
		{
			raw:  "0s",
			want: 0,
		},
		{
			raw:  "1m",
			want: 60,
		},
	} {
		t.Run("interval_"+setting.raw, func(t *testing.T) {
			t.Setenv("LOAD_WS_RECONNECT_INTERVAL", setting.raw)
			t.Setenv("LOAD_REPORT_DIR", t.TempDir())
			report, _, err := newReport("smoke")
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			var saved map[string]json.RawMessage
			if err := json.Unmarshal(body, &saved); err != nil {
				t.Fatal(err)
			}
			var seconds float64
			field, ok := saved["websocket_reconnect_interval_seconds"]
			if !ok {
				t.Fatal("report omitted the WebSocket workload interval")
			}
			if err := json.Unmarshal(field, &seconds); err != nil || seconds != setting.want {
				t.Fatalf("recorded interval=%s, err=%v; want %v seconds", field, err, setting.want)
			}
		})
	}
	for _, raw := range []string{"-1s", "invalid", "1us", "13h"} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			t.Setenv("LOAD_WS_RECONNECT_INTERVAL", raw)
			if _, _, err := newReport("smoke"); err == nil {
				t.Fatalf("invalid reconnect setting %q was accepted", raw)
			}
		})
	}
}

func TestPersistentWebSocketOutlivesHandshakeTimeout(t *testing.T) {
	server := observerWebSocketServer(t, false, `{}`)
	defer server.Close()
	o, ctx := startTestSpectator(t, server.URL, 0, 30*time.Millisecond)
	defer o.stop()
	waitForObserver(ctx, t, func() bool {
		return o.messages.Load() >= 2
	})
	if !waitContext(ctx, 150*time.Millisecond) {
		t.Fatal(ctx.Err())
	}
	if o.connections.Load() != 1 || o.failures.Load() != 0 || o.messages.Load() < 5 {
		t.Fatalf("handshake timeout shortened a persistent connection: connections=%d messages=%d failures=%d", o.connections.Load(), o.messages.Load(), o.failures.Load())
	}
}

func TestForcedWebSocketCycleStillReconnectsWithoutFailure(t *testing.T) {
	server := observerWebSocketServer(t, false, `{}`)
	defer server.Close()
	o, ctx := startTestSpectator(t, server.URL, 50*time.Millisecond, 20*time.Millisecond)
	defer o.stop()
	waitForObserver(ctx, t, func() bool {
		return o.connections.Load() >= 2
	})
	if o.failures.Load() != 0 {
		t.Fatalf("an intentional reconnect was counted as a failure: %d", o.failures.Load())
	}
}

func TestPersistentWebSocketReconnectsAfterRealServerClose(t *testing.T) {
	server := observerWebSocketServer(t, true, `{}`)
	defer server.Close()
	o, ctx := startTestSpectator(t, server.URL, 0, 100*time.Millisecond)
	defer o.stop()
	waitForObserver(ctx, t, func() bool {
		return o.connections.Load() >= 2 && o.messages.Load() > 0
	})
	if o.failures.Load() == 0 {
		t.Fatal("a real server close was hidden as an intentional reconnect")
	}
}

func TestPersistentWebSocketStillValidatesMessages(t *testing.T) {
	server := observerWebSocketServer(t, false, `{`)
	defer server.Close()
	o, ctx := startTestSpectator(t, server.URL, 0, 100*time.Millisecond)
	defer o.stop()
	waitForObserver(ctx, t, func() bool {
		return o.failures.Load() > 0
	})
	if o.messages.Load() != 0 {
		t.Fatal("invalid JSON was counted as a valid WebSocket message")
	}
}

func startTestSpectator(
	t *testing.T,
	origin string,
	interval, dialTimeout time.Duration,
) (*observers, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	o := &observers{
		cancel:            cancel,
		reconnectInterval: interval,
		dialTimeout:       dialTimeout,
	}
	o.wait.Go(func() {
		o.spectator(ctx, origin)
	})
	return o, ctx
}

func observerWebSocketServer(t *testing.T, closeFirst bool, payload string) *httptest.Server {
	t.Helper()
	var connections atomic.Uint64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		index := connections.Add(1)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(payload)); err != nil {
				return
			}
			if closeFirst && index == 1 {
				_ = conn.Close(websocket.StatusInternalError, "test reconnect")
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
			}
		}
	}))
}

func waitForObserver(ctx context.Context, t *testing.T, ready func() bool) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatal("WebSocket observer did not reach the expected state: ", ctx.Err())
		case <-tick.C:
		}
	}
}

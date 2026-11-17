package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	ws "github.com/coder/websocket"
)

func TestEventClientWritePreservesLargeUnicodeAndMessageBoundaries(t *testing.T) {
	large, err := json.Marshal(map[string]string{
		"type":    "init_scoreboard",
		"payload": strings.Repeat("Ж🚩漢字 é \\\" <&>\x00", 8192),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(large) <= 65535 || !utf8.Valid(large) {
		t.Fatal("large fixture must cross the extended frame-length boundary with valid Unicode")
	}
	payloads := [][]byte{nil, []byte("small"), []byte(`{"type":"admin_message","payload":"Ж🚩 <&> \\\""}`), large, []byte("tail")}
	completed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r, nil)
		if err != nil {
			completed <- err
			return
		}
		defer conn.CloseNow()
		client := &eventClient{
			conn: conn,
		}
		for _, payload := range payloads {
			if err := client.write(r.Context(), payload); err != nil {
				completed <- err
				return
			}
		}
		completed <- nil
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	//nolint:bodyclose // websocket.Dial owns the handshake response body.
	client, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	// This is the receiving test peer's limit; production inbound1024 is unchanged.
	client.SetReadLimit(1 << 20)
	for index, expected := range payloads {
		kind, actual, err := client.Read(ctx)
		if err != nil || kind != ws.MessageText || !bytes.Equal(actual, expected) {
			t.Fatalf("message %d kind=%v bytes=%d want=%d err=%v", index, kind, len(actual), len(expected), err)
		}
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server writer did not complete: ", ctx.Err())
	}
}

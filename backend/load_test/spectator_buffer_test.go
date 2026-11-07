package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/coder/websocket"
)

func TestSpectatorMessageBufferReadsFragmentedMessagesAndReusesStorage(t *testing.T) {
	var buffer spectatorMessageBuffer
	firstPayload := []byte(`{"type":"flag_stolen","value":"` + strings.Repeat("x", 600) + `"}`)
	first, err := buffer.read(iotest.OneByteReader(bytes.NewReader(firstPayload)))
	if err != nil || !bytes.Equal(first, firstPayload) || !json.Valid(first) {
		t.Fatalf("fragmented message changed: size=%d err=%v", len(first), err)
	}
	firstStorage := &first[0]
	secondPayload := []byte(`{"type":"flag_stolen","value":"` + strings.Repeat("y", 600) + `"}`)
	second, err := buffer.read(iotest.OneByteReader(bytes.NewReader(secondPayload)))
	if err != nil || !bytes.Equal(second, secondPayload) || &second[0] != firstStorage {
		t.Fatalf("subsequent message did not reuse spectator storage: size=%d err=%v", len(second), err)
	}
	for _, payload := range []string{
		`{`,
		`[]`,
		`{"next":true}`,
	} {
		body, err := buffer.read(strings.NewReader(payload))
		if err != nil || string(body) != payload {
			t.Fatalf("message boundary or invalid bytes changed: body=%q err=%v", body, err)
		}
	}
}

type spectatorReadError struct {
	err error
}

func (r spectatorReadError) Read(_ []byte) (int, error) {
	return 0, r.err
}

func TestSpectatorMessageBufferBoundsRetentionAndWrapsReadErrors(t *testing.T) {
	var buffer spectatorMessageBuffer
	large := `"` + strings.Repeat("x", spectatorBufferRetention+1) + `"`
	body, err := buffer.read(strings.NewReader(large))
	if err != nil || string(body) != large || !json.Valid(body) || buffer.data.Cap() != 0 {
		t.Fatalf("large message changed or remained retained: len=%d cap=%d err=%v", len(body), buffer.data.Cap(), err)
	}
	for _, want := range []error{
		errors.New("test read failure"),
		context.Canceled,
		context.DeadlineExceeded,
	} {
		partial := io.MultiReader(strings.NewReader(strings.Repeat("x", spectatorBufferRetention+1)), spectatorReadError{
			err: want,
		})
		body, err := buffer.read(partial)
		if body != nil || !errors.Is(err, want) || !strings.Contains(err.Error(), "load.spectatorMessageBuffer.read") || buffer.data.Cap() != 0 {
			t.Fatalf("partial/error result changed or remained retained: body=%d cap=%d err=%v", len(body), buffer.data.Cap(), err)
		}
	}
	body, err = buffer.read(strings.NewReader(`{}`))
	if err != nil || string(body) != `{}` || buffer.data.Cap() == 0 || buffer.data.Cap() > spectatorBufferRetention {
		t.Fatalf("buffer did not recover after a failed message: cap=%d err=%v", buffer.data.Cap(), err)
	}
}

func TestSpectatorBufferedMessagesPreserveFramingControlAndValidationCounts(t *testing.T) {
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			finished <- err
			return
		}
		defer conn.CloseNow()
		ctx := conn.CloseRead(r.Context())
		for _, payload := range []string{
			`{"first":1}`,
			`{`,
			`{"second":"Ж"}`,
		} {
			writer, err := conn.Writer(ctx, websocket.MessageText)
			if err != nil {
				finished <- err
				return
			}
			middle := len(payload) / 2
			if _, err = io.WriteString(writer, payload[:middle]); err == nil {
				_, err = io.WriteString(writer, payload[middle:])
			}
			if err = errors.Join(err, writer.Close()); err != nil {
				finished <- err
				return
			}
			if err := conn.Ping(ctx); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
		<-ctx.Done()
	}))
	defer server.Close()
	o, ctx := startTestSpectator(t, server.URL, 0, 500*time.Millisecond)
	defer o.stop()
	waitForObserver(ctx, t, func() bool {
		return o.messages.Load() == 2 && o.failures.Load() == 1
	})
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("fragmented messages or control-frame replies did not complete")
	}
	if o.connections.Load() != 1 {
		t.Fatal("multiple messages were treated as separate connections")
	}
}

func TestSpectatorBufferedMessagesKeepTheReadLimit(t *testing.T) {
	large := `"` + strings.Repeat("x", (1<<20)+1) + `"`
	server := observerWebSocketServer(t, false, large)
	defer server.Close()
	o, ctx := startTestSpectator(t, server.URL, 0, 500*time.Millisecond)
	defer o.stop()
	waitForObserver(ctx, t, func() bool {
		return o.failures.Load() > 0
	})
	stream, err := o.stop()
	if err != nil || stream.Messages != 0 || stream.Connections != 1 || !strings.Contains(stream.FirstError, "read limited") {
		t.Fatalf("oversized message bypassed the existing read limit: report=%+v err=%v", stream, err)
	}
}

func TestSpectatorBufferedPartialMessageStopsOnCancellation(t *testing.T) {
	partialWritten := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := conn.CloseRead(r.Context())
		writer, err := conn.Writer(ctx, websocket.MessageText)
		if err != nil {
			return
		}
		defer writer.Close()
		// More than the writer buffer forces a non-FIN frame onto the wire.
		// The remaining message never reaches EOF before cancellation.
		if _, err := io.WriteString(writer, `{"partial":"`+strings.Repeat("x", 8<<10)); err != nil {
			return
		}
		close(partialWritten)
		<-ctx.Done()
	}))
	defer server.Close()
	o, ctx := startTestSpectator(t, server.URL, 0, 500*time.Millisecond)
	defer o.stop()
	select {
	case <-partialWritten:
	case <-ctx.Done():
		t.Fatal("partial message was not written")
	}
	stopped := make(chan streamReport, 1)
	go func() {
		report, _ := o.stop()
		stopped <- report
	}()
	select {
	case report := <-stopped:
		if report.Connections != 1 || report.Messages != 0 || report.Failures != 0 {
			t.Fatalf("intentional cancellation changed stream accounting: %+v", report)
		}
	case <-time.After(time.Second):
		t.Fatal("partial message ignored spectator cancellation")
	}
}

func BenchmarkSpectatorMessageBuffer(b *testing.B) {
	for _, fixture := range []struct {
		name    string
		payload string
	}{
		{
			name:    "flag-stolen",
			payload: `{"type":"flag_stolen","payload":{"attacker":"team-1","victim":"team-2","service":"notes","round":42,"message":"` + strings.Repeat("x", 320) + `"}}`,
		},
		{
			name:    "status",
			payload: `{"type":"status","payload":"` + strings.Repeat("x", 2048) + `"}`,
		},
		{
			name:    "scoreboard",
			payload: `{"type":"scoreboard","payload":"` + strings.Repeat("x", 16<<10) + `"}`,
		},
	} {
		payload := []byte(fixture.payload)
		b.Run(fixture.name+"/io-readall", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			var source bytes.Reader
			for b.Loop() {
				source.Reset(payload)
				body, err := io.ReadAll(&source)
				if err != nil || !json.Valid(body) {
					b.Fatal("message read or validation failed: ", err)
				}
			}
		})
		b.Run(fixture.name+"/reuse", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			var source bytes.Reader
			var buffer spectatorMessageBuffer
			for b.Loop() {
				source.Reset(payload)
				body, err := buffer.read(&source)
				if err != nil || !json.Valid(body) {
					b.Fatal("message read or validation failed: ", err)
				}
			}
		})
	}
}

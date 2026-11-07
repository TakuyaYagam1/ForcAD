package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func outboxFlag(sequence int) []byte {
	return fmt.Appendf(nil, "{ \"type\": \"flag_stolen\", \"payload\": %d }\n", sequence)
}

func outboxState(sequence int) []byte {
	return fmt.Appendf(nil, "{ \"type\": \"init_scoreboard\", \"payload\": %d }\n", sequence)
}

func requireOutboxSend(
	t *testing.T,
	queue *eventOutbox,
	data []byte,
	wantEviction bool,
) {
	t.Helper()
	accepted, evicted := queue.send(data)
	if !accepted || evicted != wantEviction {
		t.Fatalf("enqueue accepted=%t evicted=%t; want accepted and evicted=%t", accepted, evicted, wantEviction)
	}
}

func requireOutboxContents(t *testing.T, queue *eventOutbox, want [][]byte) {
	t.Helper()
	for index, expected := range want {
		select {
		case <-queue.ready:
		case <-time.After(time.Second):
			t.Fatalf("queued event %d lost its wakeup", index)
		}
		data, ok := queue.take()
		if !ok || !bytes.Equal(data, expected) {
			t.Fatalf("event %d=%q available=%t; want exact bytes %q", index, data, ok, expected)
		}
	}
	if data, ok := queue.take(); ok {
		t.Fatalf("unexpected extra queued event %q", data)
	}
}

func TestPriorityOutboxFullNotificationsAcceptCriticalWithoutDraining(t *testing.T) {
	queue := newEventOutbox(clientSendBuffer)
	var want [][]byte
	for index := range clientSendBuffer {
		data := outboxFlag(index)
		requireOutboxSend(t, queue, data, false)
		if index > 0 {
			want = append(want, data)
		}
	}
	if accepted, evicted := queue.send(outboxFlag(999)); accepted || evicted {
		t.Fatal("a full outbox accepted another disposable notification")
	}
	critical := outboxState(1000)
	requireOutboxSend(t, queue, critical, true)
	want = append(want, critical)
	if queue.count != clientSendBuffer {
		t.Fatalf("queue count=%d; capacity must remain %d", queue.count, clientSendBuffer)
	}
	requireOutboxContents(t, queue, want)
}

func TestPriorityOutboxMixedFIFOAndWrappedEviction(t *testing.T) {
	queue := newEventOutbox(4)
	a, b, c, d, e := outboxState(1), outboxFlag(2), outboxState(3), outboxFlag(4), outboxState(5)
	for _, data := range [][]byte{a, b, c, d} {
		requireOutboxSend(t, queue, data, false)
	}
	requireOutboxSend(t, queue, e, true)
	// Older state stays ahead of the newer state; only the oldest flag disappears.
	requireOutboxContents(t, queue, [][]byte{a, c, d, e})
	for _, data := range [][]byte{a, b, c} {
		requireOutboxSend(t, queue, data, false)
	}
	if data, ok := queue.take(); !ok || !bytes.Equal(data, a) {
		t.Fatal("could not advance the ring head")
	}
	for _, data := range [][]byte{d, e} {
		requireOutboxSend(t, queue, data, false)
	}
	f := outboxState(6)
	requireOutboxSend(t, queue, f, true)
	requireOutboxContents(t, queue, [][]byte{c, d, e, f})
}

func TestPriorityOutboxKeepsUnknownAndMalformedEventsCritical(t *testing.T) {
	queue := newEventOutbox(3)
	unknown := []byte(`{"type":"future_state","payload":{"raw":"unchanged"}}`)
	malformed := []byte("{unrecognized bytes\n")
	for _, data := range [][]byte{unknown, malformed, outboxFlag(1)} {
		requireOutboxSend(t, queue, data, false)
	}
	critical := outboxState(2)
	requireOutboxSend(t, queue, critical, true)
	if accepted, evicted := queue.send(outboxState(3)); accepted || evicted {
		t.Fatal("an all-critical full outbox must reject the next critical event")
	}
	requireOutboxContents(t, queue, [][]byte{unknown, malformed, critical})
}

func TestPriorityOutboxConcurrentCloseSendAndTake(t *testing.T) {
	queue := newEventOutbox(clientSendBuffer)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	firstSent, firstTaken, race := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var sentOnce sync.Once
	for producer := range 4 {
		workers.Go(func() {
			if accepted, _ := queue.send(outboxFlag(producer)); accepted {
				sentOnce.Do(func() {
					close(firstSent)
				})
			}
			select {
			case <-race:
			case <-ctx.Done():
				return
			}
			for index := range 1000 {
				data := outboxFlag(index + producer*1000)
				if index%10 == 0 {
					data = outboxState(index)
				}
				queue.send(data)
			}
		})
	}
	workers.Go(func() {
		initial := true
		for {
			select {
			case <-queue.ready:
				if _, ok := queue.take(); ok && initial {
					initial = false
					close(firstTaken)
					select {
					case <-race:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	})
	for _, progress := range []<-chan struct{}{firstSent, firstTaken} {
		select {
		case <-progress:
		case <-ctx.Done():
			t.Fatal("no successful live send/take before close race")
		}
	}
	close(race)
	workers.Go(queue.close)
	queue.close()
	cancel()
	workers.Wait()
	if accepted, evicted := queue.send(outboxState(1)); accepted || evicted {
		t.Fatal("closed outbox accepted an event")
	}
	if data, ok := queue.take(); ok || data != nil || queue.count != 0 {
		t.Fatal("closed outbox retained delivery state")
	}
	for _, item := range queue.items {
		if item.data != nil {
			t.Fatal("close retained a queued payload reference")
		}
	}
}

func TestPriorityClientHubCountsEvictionAndCriticalOverflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	events := New(nil, slog.New(slog.DiscardHandler))
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		events.Run(ctx)
	}()
	defer func() {
		cancel()
		<-runDone
	}()
	// Deliberately keep the writer unscheduled while the real subscriber fills.
	client := &eventClient{
		hub:    events.hub,
		outbox: newEventOutbox(clientSendBuffer),
		done:   make(chan struct{}),
		onEvicted: func() {
			events.notificationsDropped.Add(1)
		},
	}
	if err := events.hub.Register(client); err != nil {
		t.Fatal(err)
	}
	waitSubscriber(t, events)
	for index := range clientSendBuffer {
		if !client.Send(outboxFlag(index)) {
			t.Fatal("ordinary queue fill rejected")
		}
	}
	if err := events.Publish(ctx, "flag_stolen", nil); err != nil {
		t.Fatal(err)
	}
	waitPriorityState(ctx, t, func() bool {
		return events.NotificationDrops() == 1
	})
	if err := events.Publish(ctx, "init_scoreboard", nil); err != nil {
		t.Fatal(err)
	}
	waitPriorityState(ctx, t, func() bool {
		return events.NotificationDrops() == 2
	})
	client.outbox.mu.Lock()
	queued := client.outbox.count
	client.outbox.mu.Unlock()
	if events.SlowClientDrops() != 0 || queued != clientSendBuffer {
		t.Fatal("notification eviction closed or expanded the state stream")
	}
	select {
	case <-client.done:
		t.Fatal("full optional queue closed when critical state arrived")
	default:
	}
	for index := range clientSendBuffer {
		data, ok := client.outbox.take()
		if !ok {
			t.Fatal("state-preserving queue lost an entry")
		}
		if index < clientSendBuffer-1 {
			if !bytes.Equal(data, outboxFlag(index+1)) {
				t.Fatal("surviving notification bytes or order changed")
			}
		} else {
			var event struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &event); err != nil || event.Type != "init_scoreboard" {
				t.Fatal("critical event was not appended to the FIFO")
			}
		}
	}
	for index := range clientSendBuffer {
		if !client.Send(outboxState(index)) {
			t.Fatal("critical-only queue fill rejected")
		}
	}
	if err := events.Publish(ctx, "init_scoreboard", nil); err != nil {
		t.Fatal(err)
	}
	waitPriorityState(ctx, t, func() bool {
		return events.SlowClientDrops() == 1
	})
	select {
	case <-client.done:
	case <-ctx.Done():
		t.Fatal("all-critical overflow was not visibly closed")
	}
	if events.NotificationDrops() != 2 {
		t.Fatal("critical overflow was miscounted as a notification drop")
	}
}

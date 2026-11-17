package websocket

import (
	"encoding/json"
	"sync"
)

type eventKind uint8

const (
	eventUnclassified eventKind = iota
	eventNotification
	eventCritical
)

type outboxEvent struct {
	data []byte
	kind eventKind
}

// eventOutbox is a bounded FIFO. Classification is deferred until overflow so
// ordinary fan-out does not decode a JSON event for every subscriber.
type eventOutbox struct {
	mu     sync.Mutex
	items  []outboxEvent
	head   int
	count  int
	closed bool
	ready  chan struct{}
}

func newEventOutbox(capacity int) *eventOutbox {
	return &eventOutbox{
		items: make([]outboxEvent, capacity),
		ready: make(chan struct{}, 1),
	}
}

func notificationEvent(data []byte) bool {
	var event struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(data, &event) == nil && event.Type == "flag_stolen"
}

func (q *eventOutbox) position(offset int) int {
	return (q.head + offset) % len(q.items)
}

func (q *eventOutbox) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// send borrows the hub's immutable event bytes. It reports an evicted queued
// notification separately; rejecting the incoming message remains a hub drop.
func (q *eventOutbox) send(data []byte) (accepted, evicted bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false, false
	}
	if q.count < len(q.items) {
		q.items[q.position(q.count)] = outboxEvent{
			data: data,
		}
		q.count++
		q.signal()
		return true, false
	}
	if notificationEvent(data) {
		return false, false
	}
	for offset := range q.count {
		item := &q.items[q.position(offset)]
		if item.kind == eventUnclassified {
			item.kind = eventCritical
			if notificationEvent(item.data) {
				item.kind = eventNotification
			}
		}
		if item.kind != eventNotification {
			continue
		}
		// Remove only the oldest disposable event and keep all remaining order.
		for next := offset; next < q.count-1; next++ {
			q.items[q.position(next)] = q.items[q.position(next+1)]
		}
		q.items[q.position(q.count-1)] = outboxEvent{
			data: data,
			kind: eventCritical,
		}
		q.signal()
		return true, true
	}
	return false, false
}

func (q *eventOutbox) take() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.count == 0 {
		return nil, false
	}
	data := q.items[q.head].data
	q.items[q.head] = outboxEvent{}
	q.head = q.position(1)
	q.count--
	if q.count > 0 {
		q.signal()
	}
	return data, true
}

func (q *eventOutbox) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	clear(q.items)
	q.head, q.count = 0, 0
}

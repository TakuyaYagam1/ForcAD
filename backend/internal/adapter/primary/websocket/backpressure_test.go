package websocket

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type queuedSubscriber struct {
	message chan []byte
	attempt chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (s *queuedSubscriber) Send(data []byte) bool {
	defer func() { s.attempt <- struct{}{} }()
	select {
	case s.message <- data:
		return true
	default:
		return false
	}
}

func (s *queuedSubscriber) Close() { s.once.Do(func() { close(s.closed) }) }

func TestNotificationOverflowPreservesStateDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	events := New(nil, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() { defer close(done); events.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	subscriber := &queuedSubscriber{message: make(chan []byte, 1), attempt: make(chan struct{}, 4), closed: make(chan struct{})}
	if err := events.hub.Register(subscriber); err != nil {
		t.Fatal(err)
	}
	waitSubscriber(t, events)
	publish := func(event string) {
		t.Helper()
		if err := events.Publish(ctx, event, map[string]int{"sequence": 1}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-subscriber.attempt:
		case <-time.After(time.Second):
			t.Fatal("event hub stopped delivering")
		}
	}
	publish("flag_stolen")
	publish("flag_stolen")
	// The third delivery also ensures the previous overflow callback finished.
	publish("flag_stolen")
	select {
	case <-subscriber.closed:
		t.Fatal("a disposable notification closed the state stream")
	default:
	}
	<-subscriber.message
	publish("init_scoreboard")
	select {
	case <-subscriber.message:
	default:
		t.Fatal("state did not reach the recovered subscriber")
	}
	publish("init_scoreboard")
	publish("init_scoreboard")
	select {
	case <-subscriber.closed:
	case <-time.After(time.Second):
		t.Fatal("losing state must force a fresh snapshot on reconnect")
	}
}

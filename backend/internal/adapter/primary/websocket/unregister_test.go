package websocket

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	wskit "github.com/wahrwelt-kit/go-wskit"
)

type unregisterBlockingSubscriber struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *unregisterBlockingSubscriber) Send([]byte) bool {
	s.once.Do(func() {
		close(s.entered)
	})
	<-s.release
	return true
}

func (*unregisterBlockingSubscriber) Close() {
}

type unregisterQueueFixture struct {
	hub              *wskit.Hub
	client           *eventClient
	timedOut         chan struct{}
	retryAllowed     chan struct{}
	clientRemoved    chan struct{}
	fillerRegistered chan struct{}
	hubDone          chan struct{}
	stopHub          context.CancelFunc
	unblockHub       func()
	allowRetry       func()
}

func waitUnregisterSignal(ctx context.Context, t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal("unregister lifecycle did not progress: ", ctx.Err())
	}
}

func fullUnregisterQueue(ctx context.Context, t *testing.T) *unregisterQueueFixture {
	t.Helper()
	f := &unregisterQueueFixture{
		timedOut:         make(chan struct{}),
		retryAllowed:     make(chan struct{}),
		clientRemoved:    make(chan struct{}),
		fillerRegistered: make(chan struct{}),
		hubDone:          make(chan struct{}),
	}
	blocker := &unregisterBlockingSubscriber{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	filler := &unregisterBlockingSubscriber{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	clientRegistered := make(chan struct{})
	var firstTimeout sync.Once
	f.hub = wskit.NewHub(
		wskit.WithRegisterBuf(1), wskit.WithChannelTimeout(10*time.Millisecond),
		wskit.WithOnConnect(func(subscriber wskit.Subscriber) {
			switch subscriber {
			case f.client:
				close(clientRegistered)
			case filler:
				close(f.fillerRegistered)
			}
		}),
		wskit.WithOnDisconnect(func(subscriber wskit.Subscriber) {
			if subscriber == f.client {
				close(f.clientRemoved)
			}
		}),
		wskit.WithOnTimeout(func(operation string) {
			if operation == "unregister" {
				firstTimeout.Do(func() {
					close(f.timedOut)
					<-f.retryAllowed
				})
			}
		}),
	)
	f.client = &eventClient{
		hub:    f.hub,
		outbox: newEventOutbox(clientSendBuffer),
		done:   make(chan struct{}),
	}
	hubCtx, stopHub := context.WithCancel(ctx)
	f.stopHub = stopHub
	var releaseHub, releaseRetry sync.Once
	f.unblockHub = func() {
		releaseHub.Do(func() {
			close(blocker.release)
		})
	}
	f.allowRetry = func() {
		releaseRetry.Do(func() {
			close(f.retryAllowed)
		})
	}
	go func() {
		defer close(f.hubDone)
		f.hub.Run(hubCtx)
	}()
	t.Cleanup(func() {
		f.unblockHub()
		f.allowRetry()
		f.stopHub()
		<-f.hubDone
	})
	if err := f.hub.Register(f.client); err != nil {
		t.Fatal(err)
	}
	waitUnregisterSignal(ctx, t, clientRegistered)
	if err := f.hub.Register(blocker); err != nil {
		t.Fatal(err)
	}
	waitPriorityState(ctx, t, func() bool {
		return f.hub.SubscriberCount() == 2
	})
	if err := f.hub.Broadcast([]byte(`{"type":"init_scoreboard"}`)); err != nil {
		t.Fatal(err)
	}
	waitUnregisterSignal(ctx, t, blocker.entered)
	// Run is inside blocker.Send; this fills its single control-queue slot.
	if err := f.hub.Register(filler); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestEventClientUnregisterRetriesFullControlQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	f := fullUnregisterQueue(ctx, t)
	finished := make(chan error, 1)
	go func() {
		finished <- f.client.unregister()
	}()
	waitUnregisterSignal(ctx, t, f.timedOut)
	f.unblockHub()
	waitUnregisterSignal(ctx, t, f.fillerRegistered)
	f.allowRetry()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("cleanup did not retry after queue recovery")
	}
	waitUnregisterSignal(ctx, t, f.clientRemoved)
	if f.hub.SubscriberCount() != 2 {
		t.Fatal("closed client remained in the hub after recovered cleanup")
	}
	select {
	case <-f.client.done:
	default:
		t.Fatal("unregistered client was not closed")
	}
}

func TestEventClientUnregisterExitsWhenHubStopsAfterTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	f := fullUnregisterQueue(ctx, t)
	finished := make(chan error, 1)
	go func() {
		finished <- f.client.unregister()
	}()
	waitUnregisterSignal(ctx, t, f.timedOut)
	// The timeout callback holds the caller after sendWithTimeout released its
	// enqueue lock. Stop the hub before allowing the next bounded attempt.
	f.stopHub()
	f.unblockHub()
	waitUnregisterSignal(ctx, t, f.hubDone)
	f.allowRetry()
	select {
	case err := <-finished:
		if !errors.Is(err, wskit.ErrHubStopped) {
			t.Fatalf("cleanup after hub stop returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("cleanup kept retrying after hub shutdown")
	}
	if f.hub.SubscriberCount() != 0 {
		t.Fatal("hub shutdown retained subscribers")
	}
}

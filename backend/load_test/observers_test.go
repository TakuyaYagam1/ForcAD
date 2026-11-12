package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type streamReport struct {
	Connections int64  `json:"connections"`
	Messages    int64  `json:"messages"`
	Failures    int64  `json:"failures"`
	FirstError  string `json:"first_error,omitempty"`
}

type observers struct {
	cancel            context.CancelFunc
	reconnectInterval time.Duration
	dialTimeout       time.Duration
	wait              sync.WaitGroup
	connections       atomic.Int64
	messages          atomic.Int64
	failures          atomic.Int64
	errorOnce         sync.Once
	err               error
	streamErrorOnce   sync.Once
	streamError       string
}

func observe(
	ctx context.Context,
	d *deployment,
	flags *catalog,
	directory string,
	spectators int,
) *observers {
	ctx, cancel := context.WithCancel(ctx)
	o := &observers{
		cancel: cancel,
	}
	interval, err := webSocketReconnectInterval()
	if err != nil {
		o.fail(err)
		return o
	}
	o.reconnectInterval = interval
	for range spectators {
		o.wait.Go(func() {
			o.spectator(ctx, d.origin)
		})
	}
	o.wait.Go(func() {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := flags.refresh(ctx, d); err != nil && ctx.Err() == nil {
					o.fail(err)
					return
				}
			}
		}
	})
	o.wait.Go(func() {
		o.sample(ctx, d, directory)
	})
	if checkerAuditEnabled(os.Getenv("LOAD_PROFILE")) {
		o.wait.Go(func() {
			o.sampleCheckerLifecycle(ctx, d, directory)
		})
	}
	if os.Getenv("LOAD_DIAGNOSTICS") == "1" {
		o.wait.Go(func() {
			o.traceHTTP(ctx, d, directory)
		})
		o.wait.Go(func() {
			o.sampleDatabase(ctx, d, directory)
		})
		o.wait.Go(func() {
			o.samplePool(ctx, d, directory)
		})
	}
	return o
}

func webSocketReconnectInterval() (time.Duration, error) {
	raw := os.Getenv("LOAD_WS_RECONNECT_INTERVAL")
	if raw == "" {
		return 20 * time.Second, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval < 0 || interval > 12*time.Hour || interval > 0 && interval < time.Millisecond {
		return 0, fmt.Errorf("load.webSocketReconnectInterval - LOAD_WS_RECONNECT_INTERVAL must be 0 or a duration between 1ms and 12h")
	}
	return interval, nil
}

func (o *observers) fail(err error) {
	o.errorOnce.Do(func() {
		o.err = err
	})
}

func (o *observers) stop() (streamReport, error) {
	o.cancel()
	o.wait.Wait()
	return streamReport{
		Connections: o.connections.Load(),
		Messages:    o.messages.Load(),
		Failures:    o.failures.Load(),
		FirstError:  o.streamError,
	}, o.err
}

func (o *observers) streamFailed(err error) {
	o.failures.Add(1)
	o.streamErrorOnce.Do(func() {
		o.streamError = time.Now().UTC().Format(time.RFC3339Nano) + " " + err.Error()
	})
}

func (o *observers) spectator(ctx context.Context, origin string) {
	var messageBuffer spectatorMessageBuffer
	for ctx.Err() == nil {
		cycle := ctx
		cancel := func() {
		}
		if o.reconnectInterval > 0 {
			cycle, cancel = context.WithTimeout(ctx, o.reconnectInterval)
		}
		dialTimeout := o.dialTimeout
		if dialTimeout <= 0 {
			dialTimeout = 20 * time.Second
		}
		dial, cancelDial := context.WithTimeout(cycle, dialTimeout)
		conn, response, err := websocket.Dial(dial, "ws"+strings.TrimPrefix(origin, "http")+"/api/events/ws", &websocket.DialOptions{
			HTTPHeader: http.Header{
				"Origin": {origin},
			},
		})
		cancelDial()
		if err != nil {
			if response != nil {
				response.Body.Close()
			}
			if ctx.Err() == nil {
				o.streamFailed(err)
			}
		} else {
			o.connections.Add(1)
			conn.SetReadLimit(1 << 20)
			for {
				_, reader, readErr := conn.Reader(cycle)
				var body []byte
				if readErr == nil {
					body, readErr = messageBuffer.read(reader)
				}
				if readErr != nil {
					if cycle.Err() == nil {
						o.streamFailed(readErr)
					}
					break
				}
				if !json.Valid(body) {
					o.streamFailed(fmt.Errorf("load.observers.spectator - invalid JSON message"))
				} else {
					o.messages.Add(1)
				}
			}
			conn.CloseNow()
		}
		cancel()
		if !waitContext(ctx, time.Second) {
			return
		}
	}
}

const spectatorBufferRetention = 64 << 10

// Each spectator owns one buffer across connections. Message bytes are borrowed
// until the next read and are consumed synchronously by json.Valid.
type spectatorMessageBuffer struct {
	data bytes.Buffer
}

func (b *spectatorMessageBuffer) read(reader io.Reader) ([]byte, error) {
	const location = "load.spectatorMessageBuffer.read"
	b.data.Reset()
	_, err := b.data.ReadFrom(reader)
	body := b.data.Bytes()
	if b.data.Cap() > spectatorBufferRetention {
		// The returned slice still owns its bytes for the caller's validation;
		// this spectator must not retain an unusually large frame afterwards.
		b.data = bytes.Buffer{}
	}
	if err != nil {
		return nil, fmt.Errorf("%s - read complete WebSocket message: %w", location, err)
	}
	return body, nil
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (o *observers) sample(ctx context.Context, d *deployment, directory string) {
	file, err := os.OpenFile(filepath.Join(directory, "resources.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		o.fail(fmt.Errorf("load.observers.sample - create samples: %w", err))
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			o.fail(err)
		}
	}()
	for ctx.Err() == nil {
		if err := sampleResources(ctx, d, json.NewEncoder(file)); err != nil && ctx.Err() == nil {
			o.fail(err)
			// Keep the failure visible without losing every later resource sample.
			if writeErr := json.NewEncoder(file).Encode(struct {
				Time  time.Time `json:"time"`
				Error string    `json:"sample_error"`
			}{time.Now().UTC(), err.Error()}); writeErr != nil {
				o.fail(writeErr)
				return
			}
		}
		if !waitContext(ctx, 10*time.Second) {
			return
		}
	}
}

func sampleResources(ctx context.Context, d *deployment, encoder *json.Encoder) error {
	ids, err := d.command(ctx, "", "ps", "-q")
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := append([]string{"stats", "--no-stream", "--format", "{{json .}}"}, strings.Fields(string(ids))...)
	if len(strings.Fields(string(ids))) == 0 {
		return fmt.Errorf("load.sampleResources - no test containers")
	}
	data, err := resourceCommandOutput(bounded, exec.CommandContext(bounded, "docker", args...))
	if err != nil {
		return fmt.Errorf("load.sampleResources - docker stats: %w", err)
	}
	var containers []json.RawMessage
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		containers = append(containers, json.RawMessage(line))
	}
	database, err := d.query(ctx, `SELECT json_build_object(
'connections',count(*),'active',count(*) FILTER(WHERE state='active'),
'lock_waits',count(*) FILTER(WHERE wait_event_type='Lock'),
'waiting',COALESCE(json_agg(json_build_object('pid',pid,'wait',wait_event,
'age_seconds',extract(epoch FROM clock_timestamp()-query_start),
'blockers',pg_blocking_pids(pid),'query_name',CASE WHEN query LIKE '-- name:%'
THEN split_part(query,E'\n',1) ELSE 'probe' END)) FILTER(WHERE state<>'idle'),'[]'))
FROM pg_stat_activity WHERE backend_type='client backend'`)
	if err != nil {
		return err
	}
	return encoder.Encode(struct {
		Time       time.Time         `json:"time"`
		Containers []json.RawMessage `json:"containers"`
		Database   json.RawMessage   `json:"database"`
	}{time.Now().UTC(), containers, json.RawMessage(database)})
}

func resourceCommandOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	cmd.WaitDelay = 2 * time.Second
	started := time.Now()
	data, err := cmd.Output()
	if err == nil {
		return data, nil
	}
	var stderr string
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		// Output retains a bounded prefix/suffix of stderr on failure. Keep it:
		// exit status 1 alone cannot distinguish a Docker error from a Windows
		// process killed after the command's context deadline.
		stderr = strings.TrimSpace(string(exitErr.Stderr))
	}
	return nil, fmt.Errorf("after %s (context=%s, stderr=%q): %w",
		time.Since(started).Round(time.Millisecond), fmt.Sprint(ctx.Err()), stderr, errors.Join(err, ctx.Err()))
}

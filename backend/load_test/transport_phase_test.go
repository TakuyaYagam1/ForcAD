package load_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

type transportPhaseEvent struct {
	Phase        string  `json:"phase"`
	Milliseconds float64 `json:"ms"`
	Reused       bool    `json:"reused,omitempty"`
	WasIdle      bool    `json:"was_idle,omitempty"`
	Failed       bool    `json:"failed,omitempty"`
}

type transportPhaseRecord struct {
	StartedUTC    time.Time             `json:"started_utc"`
	FinishedUTC   time.Time             `json:"finished_utc"`
	Sequence      string                `json:"seq"`
	Route         string                `json:"route"`
	Status        int                   `json:"status,omitempty"`
	Milliseconds  float64               `json:"duration_ms"`
	ErrorCategory string                `json:"error_category,omitempty"`
	Events        []transportPhaseEvent `json:"events"`
	DroppedEvents uint64                `json:"dropped_events"`
}

type transportPhaseReport struct {
	Seen                    uint64                 `json:"seen"`
	Completed               uint64                 `json:"completed"`
	RetainedSlow            uint64                 `json:"retained_slow"`
	RetainedTransportErrors uint64                 `json:"retained_transport_errors"`
	RetainedHTTPErrors      uint64                 `json:"retained_http_errors"`
	DroppedSlow             uint64                 `json:"dropped_slow"`
	DroppedTransportErrors  uint64                 `json:"dropped_transport_errors"`
	DroppedHTTPErrors       uint64                 `json:"dropped_http_errors"`
	Records                 []transportPhaseRecord `json:"records"`
}

type transportPhaseTransport struct {
	base                             http.RoundTripper
	mu                               sync.Mutex
	report                           transportPhaseReport
	slowLimit, errorLimit, httpLimit uint64
}

func newTransportPhaseTransport(base http.RoundTripper) *transportPhaseTransport {
	return &transportPhaseTransport{
		base:       base,
		slowLimit:  128,
		errorLimit: 128,
		httpLimit:  32,
	}
}

func (t *transportPhaseTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface {
		CloseIdleConnections()
	}); ok {
		closer.CloseIdleConnections()
	}
}

func (t *transportPhaseTransport) snapshot() *transportPhaseReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	report := t.report
	report.Records = append([]transportPhaseRecord(nil), report.Records...)
	return &report
}

func (t *transportPhaseTransport) retain(record transportPhaseRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.report.Completed++
	switch {
	case record.ErrorCategory == "http_5xx":
		if t.report.RetainedHTTPErrors >= t.httpLimit {
			t.report.DroppedHTTPErrors++
			return
		}
		t.report.RetainedHTTPErrors++
	case record.ErrorCategory != "":
		if t.report.RetainedTransportErrors >= t.errorLimit {
			t.report.DroppedTransportErrors++
			return
		}
		t.report.RetainedTransportErrors++
	default:
		if record.Milliseconds < 500 {
			return
		}
		if t.report.RetainedSlow >= t.slowLimit {
			t.report.DroppedSlow++
			return
		}
		t.report.RetainedSlow++
	}
	t.report.Records = append(t.report.Records, record)
}

type transportPhaseState struct {
	mu        sync.Mutex
	ctx       context.Context
	started   time.Time
	finished  bool
	record    transportPhaseRecord
	collector *transportPhaseTransport
}

func (s *transportPhaseState) event(event transportPhaseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	if len(s.record.Events) >= 24 {
		s.record.DroppedEvents++
		return
	}
	event.Milliseconds = milliseconds(time.Since(s.started))
	s.record.Events = append(s.record.Events, event)
}

func transportPhaseError(err error) string {
	if err == nil || errors.Is(err, io.EOF) {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return "transport_error"
}

func (s *transportPhaseState) finish(err error) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	finished := time.Now()
	s.record.FinishedUTC = finished.UTC()
	s.record.Milliseconds = milliseconds(finished.Sub(s.started))
	s.record.ErrorCategory = transportPhaseError(err)
	if s.record.ErrorCategory == "" && s.record.Status >= 500 && s.record.Status <= 599 {
		s.record.ErrorCategory = "http_5xx"
	}
	if err != nil && !errors.Is(err, io.EOF) && s.ctx != nil {
		if contextErr := s.ctx.Err(); contextErr != nil {
			s.record.ErrorCategory = transportPhaseError(contextErr)
		} else if deadline, ok := s.ctx.Deadline(); ok && !finished.Before(deadline) {
			s.record.ErrorCategory = "deadline"
		}
	}
	record := s.record
	s.mu.Unlock()
	s.collector.retain(record)
}

func transportPhaseSequence(value string) string {
	if len(value) < 1 || len(value) > 20 {
		return "-"
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return "-"
		}
	}
	return value
}

func (t *transportPhaseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	state := &transportPhaseState{
		ctx:       request.Context(),
		started:   started,
		collector: t,
		record: transportPhaseRecord{
			StartedUTC: started.UTC(),
			Sequence:   transportPhaseSequence(request.Header.Get("X-Vegeta-Seq")),
			Route:      httpTraceRoute(request.URL.Path),
			Events:     make([]transportPhaseEvent, 0, 8),
		},
	}
	t.mu.Lock()
	t.report.Seen++
	t.mu.Unlock()
	trace := &httptrace.ClientTrace{
		GetConn: func(_ string) {
			state.event(transportPhaseEvent{
				Phase: "get_conn",
			})
		},
		GotConn: func(info httptrace.GotConnInfo) {
			state.event(transportPhaseEvent{
				Phase:   "got_conn",
				Reused:  info.Reused,
				WasIdle: info.WasIdle,
			})
		},
		ConnectStart: func(_, _ string) {
			state.event(transportPhaseEvent{
				Phase: "connect_start",
			})
		},
		ConnectDone: func(_, _ string, err error) {
			state.event(transportPhaseEvent{
				Phase:  "connect_done",
				Failed: err != nil,
			})
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			state.event(transportPhaseEvent{
				Phase:  "wrote_request",
				Failed: info.Err != nil,
			})
		},
		GotFirstResponseByte: func() {
			state.event(transportPhaseEvent{
				Phase: "first_response_byte",
			})
		},
	}
	response, err := t.base.RoundTrip(request.WithContext(httptrace.WithClientTrace(request.Context(), trace)))
	state.event(transportPhaseEvent{
		Phase:  "round_trip_done",
		Failed: err != nil,
	})
	if err != nil {
		state.finish(err)
		return response, err
	}
	state.mu.Lock()
	state.record.Status = response.StatusCode
	state.mu.Unlock()
	if response.Body == nil || response.Body == http.NoBody {
		state.finish(nil)
	} else {
		response.Body = &transportPhaseBody{
			ReadCloser: response.Body,
			state:      state,
		}
	}
	return response, nil
}

type transportPhaseBody struct {
	io.ReadCloser

	state *transportPhaseState
}

func (b *transportPhaseBody) Read(buffer []byte) (int, error) {
	n, err := b.ReadCloser.Read(buffer)
	if err != nil {
		b.state.event(transportPhaseEvent{
			Phase:  "body_done",
			Failed: !errors.Is(err, io.EOF),
		})
		b.state.finish(err)
	}
	return n, err
}

func (b *transportPhaseBody) Close() error {
	err := b.ReadCloser.Close()
	b.state.finish(err)
	return err
}

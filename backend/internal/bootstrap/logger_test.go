package bootstrap

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
)

func TestLoggerFlushesStructuredChildFields(t *testing.T) {
	var output bytes.Buffer
	logger, err := NewLogger(&output)
	if err != nil {
		t.Fatal(err)
	}
	logger.With("role", "receiver").WithGroup("request").Info("accepted", "count", 2)
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := jsonv2.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["service"] != "forcad" || entry["role"] != "receiver" || entry["message"] != "accepted" || entry["request.count"] != float64(2) {
		t.Fatalf("structured log fields: %v", entry)
	}
	if _, ok := entry["request"]; ok {
		t.Fatalf("logkit group should use a flat key: %v", entry)
	}
}

func TestLoggerPreservesNestedCheckerProcessDiagnostic(t *testing.T) {
	var output bytes.Buffer
	logger, err := NewLogger(&output)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, time.October, 30, 9, 22, 18, 123456789, time.UTC)
	diagnostic := processchecker.ProcessDiagnostic{
		Phase: "unreaped", PID: 2307, RequestedAt: timestamp, CollectedAt: timestamp.Add(time.Millisecond),
		FinishedAt: timestamp.Add(2 * time.Millisecond), TimedOut: true, ExitCode: -1, SnapshotComplete: true,
		Signals: []processchecker.ProcessSignal{
			{Time: timestamp, Signal: "SIGTERM", Target: "process_group", PID: 2307, PGID: 2307, Outcome: "success"},
			{Time: timestamp, Signal: "SIGKILL", Target: "process_group", PID: 2307, PGID: 2307, Outcome: "ESRCH", Error: "no such process"},
		},
		Proc: &processchecker.ProcessSnapshot{PID: 2307, PPID: 1, PGID: 2307, State: "D", StartTimeTicks: 29757,
			Status: map[string]string{"Threads": "1"}, Wchan: "__vma_start_write", Errors: map[string]string{"stack": "permission_denied"}},
		Goroutines: "goroutine 2048 [syscall]:\nos.(*Process).pidfdWait\n",
	}
	logger.Warn("checker process diagnostic", "job_id", "test-job", "diagnostic", diagnostic)
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	var entry struct {
		JobID      string                           `json:"job_id"`
		Diagnostic processchecker.ProcessDiagnostic `json:"diagnostic"`
	}
	if err := jsonv2.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("diagnostic must be a JSON object with nested process and signal data: %v", err)
	}
	// logkit intentionally normalizes control characters in every string field.
	diagnostic.Goroutines = strings.ReplaceAll(diagnostic.Goroutines, "\n", " ")
	if entry.JobID != "test-job" || !reflect.DeepEqual(entry.Diagnostic, diagnostic) {
		t.Fatalf("diagnostic lost nested fields or stack content: got %+v, want %+v", entry.Diagnostic, diagnostic)
	}
}

func TestLoggerRedactsGroupedCredentials(t *testing.T) {
	var output bytes.Buffer
	logger, err := NewLogger(&output)
	if err != nil {
		t.Fatal(err)
	}
	logger.WithGroup("request").Info("handled", "authorization", "private-bearer-value")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("private-bearer-value")) || !bytes.Contains(output.Bytes(), []byte(`"request.authorization":"[REDACTED]"`)) {
		t.Fatalf("grouped credential was not redacted: %s", output.Bytes())
	}
}

type shortLogWriter struct{}

func (shortLogWriter) Write(data []byte) (int, error) { return len(data) / 2, nil }

func TestLoggerReportsOutputFailureOnClose(t *testing.T) {
	logger, err := NewLogger(shortLogWriter{})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("entry")
	if err := logger.Close(); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Close() error = %v, want short write", err)
	}
	if logger.dropped.Load() != 1 {
		t.Fatal("failed output was not counted")
	}
}

func TestLoggerConcurrentCloseAndWrite(t *testing.T) {
	logger, err := NewLogger(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	for range 8 {
		writers.Go(func() {
			for range 100 {
				logger.Info("entry")
			}
		})
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	writers.Wait()
	if err := logger.Close(); err != nil {
		t.Fatalf("repeated Close(): %v", err)
	}
}

type blockedLogWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (writer *blockedLogWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() { close(writer.entered) })
	<-writer.release
	return len(data), nil
}

func TestLoggingDoesNotBlockRequestsOnSlowOutput(t *testing.T) {
	writer := &blockedLogWriter{entered: make(chan struct{}), release: make(chan struct{})}
	logger, err := NewLogger(writer)
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(writer.release) }); _ = logger.Close() })
	go logger.Info("first entry")
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("output was not reached")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range logBufferCapacity * 2 {
			logger.Info("request completed")
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("requests waited for the blocked log sink")
	}
	release.Do(func() { close(writer.release) })
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if logger.dropped.Load() == 0 {
		t.Fatal("overflow must be observable instead of silently losing logs")
	}
}

func TestLoggerCloseBoundsBlockedOutput(t *testing.T) {
	writer := &blockedLogWriter{entered: make(chan struct{}), release: make(chan struct{})}
	logger, err := NewLogger(writer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(writer.release); _ = logger.Close() })
	logger.Info("entry")
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("output was not reached")
	}
	done := make(chan error, 1)
	go func() { done <- logger.Close() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked output must report an incomplete drain")
		}
	case <-time.After(logCloseTimeout + time.Second):
		t.Fatal("logger shutdown waited indefinitely for the output")
	}
}

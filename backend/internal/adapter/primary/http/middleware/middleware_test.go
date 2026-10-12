package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestIDRejectsUnsafeValues(t *testing.T) {
	for _, id := range []string{"", "bad\r\nvalue", strings.Repeat("a", 129), "bad value", "request-42"} {
		t.Run(id, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("X-Request-ID", id)
			writer := httptest.NewRecorder()
			RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Context().Value(requestIDKey{}); got != w.Header().Get("X-Request-ID") {
					t.Fatalf("request ID context/header mismatch: %v", got)
				}
			})).ServeHTTP(writer, request)
			got := writer.Header().Get("X-Request-ID")
			if !validRequestID(got) || id == "request-42" && got != id {
				t.Fatalf("unsafe or replaced valid request ID: %q", got)
			}
		})
	}
}

func TestRequestLoggerOmitsQueryAndRawPath(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestID(Logger(logger, nil, func(*http.Request) string { return "unmatched" })(http.NotFoundHandler()))
	request := httptest.NewRequest(http.MethodGet, "/private-value?token=secret-value", nil)
	request.Header.Set("Authorization", "Bearer private-credential")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["path"] != "unmatched" || entry["status"] != float64(404) || entry["request_id"] == "" {
		t.Fatalf("request log: %v", entry)
	}
	for _, secret := range []string{"private-value", "secret-value", "private-credential"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("log contains %q", secret)
		}
	}
}

func TestRecovererPreservesCommittedResponse(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	for _, committed := range []bool{false, true} {
		writer := httptest.NewRecorder()
		handler := Recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if committed {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("accepted"))
			}
			panic("private failure detail")
		}))
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
		if committed {
			if writer.Code != http.StatusAccepted || writer.Body.String() != "accepted" {
				t.Fatalf("committed response changed: %d %s", writer.Code, writer.Body.String())
			}
		} else if writer.Code != http.StatusInternalServerError || !json.Valid(writer.Body.Bytes()) || strings.Contains(writer.Body.String(), "private failure") {
			t.Fatalf("panic response: %d %s", writer.Code, writer.Body.String())
		}
	}
}

func TestRecovererPreservesAbortSignal(t *testing.T) {
	defer func() {
		if got, ok := recover().(error); !ok || !errors.Is(got, http.ErrAbortHandler) {
			t.Fatalf("panic = %v, want HTTP abort signal", got)
		}
	}()
	handler := Recoverer(slog.Default())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestContextTimeoutCancelsInlineHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	ContextTimeout(time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		if r.Context().Err() != context.DeadlineExceeded {
			t.Fatalf("request error: %v", r.Context().Err())
		}
	})).ServeHTTP(httptest.NewRecorder(), request)
	if request.Context().Err() != nil {
		t.Fatal("middleware canceled the parent context")
	}
}

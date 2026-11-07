package load_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestTransportErrorSamplesAreBoundedAndPreserveNumericMetadata(t *testing.T) {
	var report attackReport
	report.recordTransportError(&vegeta.Result{
		Code: 503,
	})
	stamp := time.Unix(1700000000, 123).UTC()
	for sequence := range uint64(20) {
		result := &vegeta.Result{
			Seq:       sequence,
			Timestamp: stamp,
			Latency:   1250 * time.Microsecond,
			URL:       fmt.Sprintf("http://example.test/flags?load_request=%d", sequence),
			Error:     "EOF",
		}
		report.recordTransportError(result)
		if result.Error != "EOF" || result.URL == "" {
			t.Fatal("sampling changed the result seen by validation/metrics")
		}
	}
	if report.TransportErrorsTotal != 20 || report.TransportErrorSamplesTruncated != 4 || len(report.TransportErrorSamples) != 16 {
		t.Fatal("transport sample ceiling or total/truncated accounting changed")
	}
	for index, sample := range report.TransportErrorSamples {
		if sample.Sequence != uint64(index) || sample.Timestamp != stamp || sample.LatencyMS != 1.25 || sample.Category != "eof" || sample.LoadRequestID == nil || *sample.LoadRequestID != uint64(index) {
			t.Fatal("sample numeric metadata changed")
		}
	}
}

func TestTransportErrorSamplesExcludeOnlyHTTPStatusErrors(t *testing.T) {
	for _, test := range []struct {
		name     string
		code     uint16
		message  string
		category string
	}{
		{
			name:    "expected limiter",
			code:    429,
			message: "429 Too Many Requests",
		},
		{
			name:    "server unavailable status",
			code:    503,
			message: "503 Service Unavailable",
		},
		{
			name:    "custom status phrase",
			code:    403,
			message: "403 Custom denial",
		},
		{
			name:    "unknown status phrase",
			code:    599,
			message: "599 Custom overload",
		},
		{
			name:    "status without reason",
			code:    429,
			message: "429",
		},
		{
			name: "healthy",
			code: 200,
		},
		{
			name:     "request EOF",
			message:  "EOF",
			category: "eof",
		},
		{
			name:     "body error after successful headers",
			code:     200,
			message:  "read: unexpected EOF",
			category: "eof",
		},
		{
			name:     "body error after limiter headers",
			code:     429,
			message:  "read tcp: connection reset by peer",
			category: "reset",
		},
		{
			name:     "body error after unavailable headers",
			code:     503,
			message:  "context deadline exceeded",
			category: "deadline",
		},
		{
			name:     "different numeric code",
			code:     429,
			message:  "503 Service Unavailable",
			category: "other",
		},
		{
			name:     "numeric prefix is not status",
			code:     429,
			message:  "4290 read failure",
			category: "other",
		},
		{
			name:     "successful code does not suppress error",
			code:     200,
			message:  "200 body failure",
			category: "other",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := &vegeta.Result{
				Code:  test.code,
				Error: test.message,
				URL:   "http://example.test/flags?load_request=42",
			}
			var report attackReport
			report.recordTransportError(result)
			if result.Code != test.code || result.Error != test.message {
				t.Fatal("sampling changed native HTTP status/error semantics")
			}
			if test.category == "" {
				if report.TransportErrorsTotal != 0 || report.TransportErrorSamplesTruncated != 0 || len(report.TransportErrorSamples) != 0 {
					t.Fatal("ordinary HTTP status error was mislabeled as transport")
				}
				return
			}
			if report.TransportErrorsTotal != 1 || report.TransportErrorSamplesTruncated != 0 || len(report.TransportErrorSamples) != 1 || report.TransportErrorSamples[0].Category != test.category {
				t.Fatal("request/body error was suppressed or misclassified")
			}
		})
	}
}

func TestHTTPStatusErrorsDoNotConsumeTransportSampleCapacity(t *testing.T) {
	var report attackReport
	for range 32 {
		report.recordTransportError(&vegeta.Result{
			Code:  429,
			Error: "429 Too Many Requests",
		})
	}
	report.recordTransportError(&vegeta.Result{
		Error: "EOF",
	})
	if report.TransportErrorsTotal != 1 || report.TransportErrorSamplesTruncated != 0 || len(report.TransportErrorSamples) != 1 || report.TransportErrorSamples[0].Category != "eof" {
		t.Fatal("HTTP rejections consumed the bounded transport-error sample budget")
	}
}

func TestTransportErrorSampleRedactsAllInputStrings(t *testing.T) {
	const secret = "PRIVATE_CANARY"
	address := "https://" + secret + ":password@example.test/" + secret + "?token=" + secret + "&load_request=42"
	var report attackReport
	report.recordTransportError(&vegeta.Result{
		URL:   address,
		Error: fmt.Sprintf("Put %q: %s header/body: EOF", address, secret),
	})
	body, err := json.Marshal(report.TransportErrorSamples)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, "password", "https", "example.test", "header/body", "EOF", "token"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatal("sample retained URL/error/header/body/credential text")
		}
	}
	var saved []map[string]json.RawMessage
	if err := json.Unmarshal(body, &saved); err != nil || len(saved) != 1 || len(saved[0]) != 5 || string(saved[0]["load_request_id"]) != "42" {
		t.Fatal("redacted schema changed or correlation ID was not numeric")
	}
}

func TestTransportErrorCategoriesAndSafeCorrelationParsing(t *testing.T) {
	for message, want := range map[string]string{
		"EOF":                       "eof",
		"read: unexpected EOF":      "eof",
		"context deadline exceeded": "deadline",
		"Get \"http://example.test/\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)": "deadline",
		"read tcp: connection reset by peer": "reset",
		"read: broken pipe":                  "other",
		"Get \"http://example.test/context deadline exceeded/EOF\": unknown failure": "other",
	} {
		if got := transportErrorCategory(message); got != want {
			t.Errorf("category=%q; want %q", got, want)
		}
	}
	for _, query := range []string{"", "load_request=", "load_request=-1", "load_request=+1", "load_request=private", "load_request=18446744073709551616", "load_request=1&load_request=2", "load_request=%zz"} {
		if transportLoadRequestID("http://example.test/?"+query) != nil {
			t.Error("invalid/ambiguous correlation value was retained")
		}
	}
	if transportLoadRequestID("http://%zz/?load_request=1") != nil {
		t.Error("malformed URL produced correlation")
	}
}

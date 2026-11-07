package load_test

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const transportErrorSampleLimit = 16

type transportErrorSample struct {
	Sequence      uint64    `json:"seq"`
	Timestamp     time.Time `json:"timestamp"`
	LatencyMS     float64   `json:"latency_ms"`
	Category      string    `json:"category"`
	LoadRequestID *uint64   `json:"load_request_id,omitempty"`
}

// Healthy results, HTTP status errors and overflow require no URL parsing.
// The single result collector calls this before clearing Result.Error;
// no input strings are retained.
func (r *attackReport) recordTransportError(result *vegeta.Result) {
	if result.Error == "" || isHTTPStatusError(result) {
		return
	}
	r.TransportErrorsTotal++
	if len(r.TransportErrorSamples) >= transportErrorSampleLimit {
		r.TransportErrorSamplesTruncated++
		return
	}
	r.TransportErrorSamples = append(r.TransportErrorSamples, transportErrorSample{
		Sequence:      result.Seq,
		Timestamp:     result.Timestamp,
		LatencyMS:     milliseconds(result.Latency),
		Category:      transportErrorCategory(result.Error),
		LoadRequestID: transportLoadRequestID(result.URL),
	})
}

// Vegeta v12.13.0 uses Error=r.Status for non-2xx/3xx responses. Match that
// status explicitly instead of excluding all nonzero codes: a request/body
// error with an available response code must still be sampled.
func isHTTPStatusError(result *vegeta.Result) bool {
	if result.Code == 0 || (result.Code >= 200 && result.Code < 400) {
		return false
	}
	code := strconv.Itoa(int(result.Code))
	return result.Error == code || strings.HasPrefix(result.Error, code+" ")
}

// Vegeta exposes strings, not original typed errors. Strip a quoted HTTP URL
// before matching known causes into fixed categories, without retaining it.
func transportErrorCategory(message string) string {
	if quote := strings.IndexByte(message, '"'); quote >= 0 {
		quoted := message[quote:]
		if prefix, err := strconv.QuotedPrefix(quoted); err == nil && strings.HasPrefix(quoted[len(prefix):], ": ") {
			message = quoted[len(prefix)+2:]
		}
	}
	message = strings.ToLower(strings.TrimSpace(message))
	switch {
	case message == "eof" || strings.HasSuffix(message, ": eof") || strings.HasSuffix(message, "unexpected eof"):
		return "eof"
	case strings.Contains(message, "context deadline exceeded") || strings.Contains(message, "client.timeout") || strings.Contains(message, "i/o timeout"):
		return "deadline"
	case strings.Contains(message, "connection reset by peer") || strings.Contains(message, "connection was forcibly closed"):
		return "reset"
	default:
		return "other"
	}
}

func transportLoadRequestID(rawURL string) *uint64 {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(query["load_request"]) != 1 {
		return nil
	}
	value := query["load_request"][0]
	if value == "" {
		return nil
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return nil
		}
	}
	identifier, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil
	}
	return &identifier
}

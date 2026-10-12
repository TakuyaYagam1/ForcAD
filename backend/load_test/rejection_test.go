package load_test

import (
	"net/http"
	"testing"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestRejectionReason(t *testing.T) {
	for _, test := range []struct {
		name string
		code uint16
		body string
		want string
	}{
		{name: "success", code: http.StatusOK, body: `[]`},
		{name: "rate", code: http.StatusTooManyRequests, want: "rate_limit"},
		{name: "capacity", code: http.StatusServiceUnavailable, body: `{"error":"Service capacity exceeded"}`, want: "capacity"},
		{name: "deadline", code: http.StatusServiceUnavailable, body: `{"error":"Request deadline exceeded"}`, want: "deadline"},
		{name: "paused", code: http.StatusServiceUnavailable, body: `{"error":"Game is paused"}`, want: "paused"},
		{name: "unavailable", code: http.StatusServiceUnavailable, body: `{"error":"Service temporarily unavailable"}`, want: "unavailable"},
		{name: "unknown", code: http.StatusServiceUnavailable, body: `{"error":"private error with a unique value"}`, want: "other"},
		{name: "proxy", code: http.StatusServiceUnavailable, body: `<html>unavailable</html>`, want: "other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := rejectionReason(&vegeta.Result{Code: test.code, Body: []byte(test.body)})
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

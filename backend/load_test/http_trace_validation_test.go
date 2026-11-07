package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func httpTraceFixtureLine(route string, status int, seconds string) string {
	return fmt.Sprintf(`2026-11-05T13:17:07.839981Z 127.0.0.1 - user [05/Nov/2026:13:17:07 +0000] "GET %s HTTP/1.1" %d 123 "secret-referer" "secret-agent" "secret-forwarded" rt=%s uct="0.001" uht="-" urt="3.023" msec=1793884627.815 seq=123456 upstream="172.20.0.10:8000" rid=0123456789abcdef0123456789abcdef cid=789`, route, status, seconds)
}

func TestHTTPTraceRetainsCorrelatedSlowRequestsWithoutSecrets(t *testing.T) {
	line := httpTraceFixtureLine("/api/client/attack_data/?token=secret-token", 499, "3.023")
	event, parsed, retain := parseHTTPTrace(line)
	if !parsed || !retain || event.Status != 499 || event.Route != "/api/client/attack_data/" || event.Sequence != "123456" || event.Connection != "789" || event.Upstream != "172.20.0.10:8000" || event.RequestSeconds != 3.023 {
		t.Fatalf("correlation or timing was lost: %+v", event)
	}
	if event.LoggedUTC == nil || event.LoggedUTC.Format(time.RFC3339Nano) != "2026-11-05T13:17:07.839981Z" {
		t.Fatal("Docker source timestamp was lost")
	}
	if event.NginxUTC == nil || event.NginxUTC.Format(time.RFC3339Nano) != "2026-11-05T13:17:07.815Z" {
		t.Fatal("Nginx log-phase timestamp was lost or rounded")
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "token") {
		t.Fatalf("trace retained raw request material: %s", data)
	}
}

func TestHTTPTraceEpochBounds(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want string
	}{
		{"0.000", "1970-01-01T00:00:00Z"},
		{"1793884627.001", "2026-11-05T13:17:07.001Z"},
		{"253402300799.999", "9999-12-31T23:59:59.999Z"},
	} {
		stamp, ok := httpTraceEpoch(test.raw)
		if !ok || stamp.Format(time.RFC3339Nano) != test.want || stamp.Location() != time.UTC {
			t.Fatalf("epoch %q: got %s, valid=%t, want %s", test.raw, stamp, ok, test.want)
		}
	}
	line := httpTraceFixtureLine("/api/client/attack_data/?token=secret-token", 499, "3.023")
	for _, raw := range []string{
		"-1.000", "+1.000", "NaN", "Inf", "1e9.000", "1793884627", "1793884627.1",
		"1793884627.0001", "253402300800.000", "999999999999.999", "1000000000000.000",
		"1793884627.secret-token", "1793884627.001.000", "1793884627. 00", "",
	} {
		if _, ok := httpTraceEpoch(raw); ok {
			t.Fatalf("malformed/out-of-range epoch %q accepted", raw)
		}
		invalid := strings.Replace(line, "msec=1793884627.815", "msec="+raw, 1)
		event, parsed, retain := parseHTTPTrace(invalid)
		if parsed || retain || event.NginxUTC != nil {
			t.Fatalf("invalid epoch %q retained: %+v", raw, event)
		}
		data, err := json.Marshal(event)
		if err != nil || strings.Contains(string(data), "secret") {
			t.Fatalf("invalid epoch leaked raw fields: %s, %v", data, err)
		}
	}
}

func TestHTTPTraceBoundsAndNormalizesUntrustedFields(t *testing.T) {
	line := httpTraceFixtureLine("/private/secret-path?token=secret-token", 500, "0.001")
	line = strings.Replace(line, `uct="0.001"`, `uct="secret-timing"`, 1)
	line = strings.Replace(line, `upstream="172.20.0.10:8000"`, `upstream="secret-host:8000"`, 1)
	event, parsed, retain := parseHTTPTrace(line)
	if !parsed || !retain || event.Route != "other" || event.Upstream != "-" || event.ConnectSeconds != "-" {
		t.Fatalf("unsafe fields were not normalized: %+v", event)
	}
	data, _ := json.Marshal(event)
	if strings.Contains(string(data), "secret") {
		t.Fatalf("unknown raw fields leaked: %s", data)
	}
	for _, invalid := range []string{strings.Repeat("x", 17<<10), strings.Replace(line, "seq=123456", "seq=123456789012345678901", 1)} {
		if _, parsed, retain := parseHTTPTrace(invalid); parsed || retain {
			t.Fatal("oversized input or unbounded sequence was accepted")
		}
	}
}

func TestHTTPTraceSelectsOnlySlowOrErrorAccessEvents(t *testing.T) {
	for _, test := range []struct {
		status  int
		seconds string
		want    bool
	}{
		{200, "0.499", false},
		{200, "0.500", true},
		{429, "0.001", false},
		{499, "0.001", true},
		{502, "0.001", true},
	} {
		_, parsed, retained := parseHTTPTrace(httpTraceFixtureLine("/flags", test.status, test.seconds))
		if !parsed || retained != test.want {
			t.Fatalf("status=%d rt=%s: retained=%t, want=%t", test.status, test.seconds, retained, test.want)
		}
	}
	line := `2026-11-05T13:17:07Z 2026/11/05 13:17:07 [error] 12#12: *789 upstream timed out secret-debug-data, request: "GET /api/client/attack_data/?token=secret HTTP/1.1", upstream: "http://172.20.0.10:8000/secret"`
	event, parsed, retained := parseHTTPTrace(line)
	if !parsed || !retained || event.Category != "upstream_timed_out" || event.Connection != "789" || event.Route != "/api/client/attack_data/" {
		t.Fatalf("error was not safely correlated: %+v", event)
	}
}

func TestHTTPTraceProcessFixture(t *testing.T) {
	t.Helper()
	mode := os.Getenv("FORCAD_HTTP_TRACE_FIXTURE")
	if mode == "" {
		return
	}
	switch mode {
	case "1":
		fmt.Println(httpTraceFixtureLine("/api/client/status/", 200, "0.001"))
		fmt.Println(httpTraceFixtureLine("/api/client/attack_data/?secret=hidden", 499, "3.023"))
	case "unrecognized":
		fmt.Println(strings.Split(httpTraceFixtureLine("/api/client/status/", 200, "0.001"), " rt=")[0])
		fmt.Println(httpTraceFixtureLine("/api/client/status/", 200, "0.001"))
		fmt.Println(`2026-11-05T13:17:07Z 2026/11/05 13:17:07 [error] 12#12: *789 fixture barrier`)
	case "empty":
		fmt.Println(`2026-11-05T13:17:07Z /docker-entrypoint.sh: configuration complete`)
		fmt.Println(`2026-11-05T13:17:07Z 2026/11/05 13:17:07 [error] 12#12: *789 fixture barrier`)
	default:
		os.Exit(2)
	}
	time.Sleep(15 * time.Second)
	os.Exit(0)
}

func TestHTTPTraceCancellationReapsStreamingProcessAndRecordsCounts(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPTraceProcessFixture$")
	cmd.Env = append(os.Environ(), "FORCAD_HTTP_TRACE_FIXTURE=1")
	reader, writer := io.Pipe()
	finished := make(chan error, 1)
	go func() {
		finished <- streamHTTPTrace(ctx, cmd, json.NewEncoder(writer))
		_ = writer.Close()
	}()
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	var event httpTraceEvent
	if err := decoder.Decode(&event); err != nil || event.Status != 499 {
		t.Fatalf("stream did not retain the slow event: %+v, %v", event, err)
	}
	cancel()
	if err := decoder.Decode(&event); err != nil || event.Event != "stop" || event.Seen != 2 || event.Parsed != 2 || event.AccessSeen != 2 || event.AccessParsed != 2 || event.Retained != 1 || event.Category != "canceled" {
		t.Fatalf("capture counts or shutdown reason were lost: %+v, %v", event, err)
	}
	select {
	case err := <-finished:
		if err != nil || cmd.ProcessState == nil {
			t.Fatalf("stream process not reaped cleanly: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("trace cancellation did not finish")
	}
}

func TestHTTPTraceCancellationRejectsMissingOrUnrecognizedAccess(t *testing.T) {
	for _, test := range []struct {
		mode     string
		category string
		seen     uint64
		parsed   uint64
	}{
		{"empty", "missing_access", 0, 0},
		{"unrecognized", "unparsed_access", 2, 1},
	} {
		t.Run(test.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPTraceProcessFixture$")
			cmd.Env = append(os.Environ(), "FORCAD_HTTP_TRACE_FIXTURE="+test.mode)
			reader, writer := io.Pipe()
			finished := make(chan error, 1)
			go func() {
				finished <- streamHTTPTrace(ctx, cmd, json.NewEncoder(writer))
				_ = writer.Close()
			}()
			defer reader.Close()
			decoder := json.NewDecoder(reader)
			var event httpTraceEvent
			if err := decoder.Decode(&event); err != nil || event.Event != "error" {
				t.Fatalf("fixture did not reach its stream barrier: %+v, %v", event, err)
			}
			cancel()
			if err := decoder.Decode(&event); err != nil || event.Event != "stop" || event.Category != test.category || event.AccessSeen != test.seen || event.AccessParsed != test.parsed {
				t.Fatalf("invalid access capture was not identified: %+v, %v", event, err)
			}
			select {
			case err := <-finished:
				if err == nil || cmd.ProcessState == nil || strings.Contains(err.Error(), "fixture barrier") {
					t.Fatalf("invalid capture was not rejected safely: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("invalid capture cancellation did not finish")
			}
		})
	}
}

func TestHTTPTraceUnexpectedEOFIsReportedWithoutRawOutput(t *testing.T) {
	var output bytes.Buffer
	err := streamHTTPTrace(t.Context(), diagnosticFixture(t.Context(), "finite", ""), json.NewEncoder(&output))
	if err == nil || !strings.Contains(err.Error(), "unexpected_eof") || strings.Contains(output.String(), "first") {
		t.Fatalf("EOF handling leaked or hid output: error=%v output=%s", err, &output)
	}
}

package load_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAttackRecordsDrainAndPostprocessTimingWithoutChangingRate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if waitContext(r.Context(), 20*time.Millisecond) {
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	const duration = 150 * time.Millisecond
	outerStart := time.Now()
	report := attack(t.Context(), attackOptions{
		rps:      100,
		duration: duration,
		workers:  1,
		target:   readTarget(server.URL, "/"),
		validate: validateRead(false),
	})
	outerElapsed := time.Since(outerStart).Seconds()
	if report.Requests == 0 || report.StartedUTC == nil || report.LastRequestStartUTC == nil || report.DrainedUTC == nil {
		t.Fatalf("missing attack timing: %+v", report)
	}
	if report.StartedUTC.Location() != time.UTC || report.LastRequestStartUTC.Location() != time.UTC || report.DrainedUTC.Location() != time.UTC {
		t.Fatal("serialized attack timestamps must be UTC")
	}
	if report.LastRequestStartUTC.Before(*report.StartedUTC) || report.DrainedUTC.Before(*report.LastRequestStartUTC) {
		t.Fatal("request-start and drain timestamps are out of order")
	}
	if report.MonotonicElapsedToDrainSeconds <= 0 || report.PostprocessElapsedSeconds < 0 {
		t.Fatal("invalid monotonic timing measurements")
	}
	if report.MonotonicElapsedToDrainSeconds+report.PostprocessElapsedSeconds > outerElapsed {
		t.Fatal("attack timing exceeds its enclosing measured duration")
	}
	wantSpan := max(duration.Seconds(), report.LastRequestStartUTC.Sub(*report.StartedUTC).Seconds())
	if math.Abs(report.RateSpanSeconds-wantSpan) > 0.001 {
		t.Fatalf("rate span=%fs, want existing nominal/latest-start span=%fs", report.RateSpanSeconds, wantSpan)
	}
	if report.AchievedRPS != float64(report.Requests)/report.RateSpanSeconds {
		t.Fatal("recorded span does not reproduce achieved RPS")
	}
	if report.Failures != 0 || report.GeneratorValid || report.SLOPassed {
		t.Fatal("timing instrumentation changed the under-delivered fixture's result")
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var restored attackReport
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.DrainedUTC == nil || !restored.DrainedUTC.Equal(*report.DrainedUTC) || restored.RateSpanSeconds != report.RateSpanSeconds {
		t.Fatal("timing fields did not survive report serialization")
	}
}

func TestAttackTimingFieldsRemainOptionalForLegacyReports(t *testing.T) {
	var report attackReport
	if err := json.Unmarshal([]byte(`{"requests":1,"achieved_rps":1}`), &report); err != nil {
		t.Fatal(err)
	}
	if report.StartedUTC != nil || report.LastRequestStartUTC != nil || report.DrainedUTC != nil || report.RateSpanSeconds != 0 {
		t.Fatal("legacy report invented attack timing")
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"started_utc", "last_request_start_utc", "drained_utc",
		"monotonic_elapsed_to_drain_seconds", "postprocess_elapsed_seconds", "rate_span_seconds",
	} {
		if _, present := fields[name]; present {
			t.Fatalf("legacy report acquired a fabricated %s field", name)
		}
	}
}

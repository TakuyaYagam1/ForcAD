//go:build linux

package load_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAttackLinuxClockAnchorsEncloseMeasuredAttack(t *testing.T) {
	var before, after unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &before); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	report := attack(t.Context(), attackOptions{
		rps:      100,
		duration: 30 * time.Millisecond,
		workers:  1,
		target:   readTarget(server.URL, "/"),
		validate: validateRead(false),
	})
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &after); err != nil {
		t.Fatal(err)
	}
	clock := report.LinuxClock
	if clock.Error != "" || clock.StartBeforeNS < before.Nano() || clock.StartBeforeNS >= clock.DrainAfterNS || clock.DrainAfterNS > after.Nano() {
		t.Fatalf("clock anchors do not enclose the measured attack: before=%d clock=%+v after=%d", before.Nano(), clock, after.Nano())
	}
	if clock.BootID != clock.DrainBootID || clock.TimeNamespace != clock.DrainTimeNamespace {
		t.Fatalf("clock domain was not preserved: %+v", clock)
	}
	if report.StartedUTC == nil || report.DrainedUTC == nil || report.MonotonicElapsedToDrainSeconds <= 0 || report.Requests == 0 {
		t.Fatalf("clock anchors changed existing attack measurements: %+v", report)
	}
}

func TestAttackLinuxClockIdentifiesKernelAndTimeNamespace(t *testing.T) {
	clock, err := readAttackLinuxClock()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(clock.bootID) {
		t.Fatalf("kernel boot identity is malformed: %+v", clock)
	}
	if !regexp.MustCompile(`^time:\[[0-9]+\]$`).MatchString(clock.timeNamespace) {
		t.Fatalf("time namespace is malformed: %+v", clock)
	}
}

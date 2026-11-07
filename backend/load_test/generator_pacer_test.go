package load_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type pacingPolicy string

const (
	pacingDiscardBacklog pacingPolicy = "discard-backlog"
	pacingRecentWindow   pacingPolicy = "recent-window"
)

// boundedPacer keeps the original rate timeline. Its default policy discards
// the entire due backlog after a stall beyond the catchup budget. The explicit
// recent-window experiment retains only due slots within that same budget.
type boundedPacer struct {
	policy        pacingPolicy
	rps           int
	catchupBudget time.Duration
	skipped       uint64
	discardEvents uint64
	maxLateness   time.Duration
	schedule      *startSchedule
}

func newBoundedPacer(rps int, catchupBudget time.Duration) *boundedPacer {
	if catchupBudget <= 0 {
		catchupBudget = 10 * time.Millisecond
	}
	return &boundedPacer{
		policy:        pacingDiscardBacklog,
		rps:           rps,
		catchupBudget: catchupBudget,
	}
}

func (p *boundedPacer) Pace(elapsed time.Duration, hits uint64) (time.Duration, bool) {
	if p.rps <= 0 {
		return 0, true
	}
	slot := hits + p.skipped + 1
	next := time.Duration(slot * uint64(time.Second) / uint64(p.rps))
	late := elapsed - next
	p.maxLateness = max(p.maxLateness, late)
	if late > p.catchupBudget {
		p.discardEvents++
		missed := uint64(elapsed)*uint64(p.rps)/uint64(time.Second) - slot + 1
		if p.policy == pacingRecentWindow {
			// Drop only slots with integer-nanosecond due times strictly before
			// elapsed-budget: floor(slot*1s/rps)>=cutoff inverts with ceil.
			cutoff := uint64(elapsed-p.catchupBudget) * uint64(p.rps)
			firstValid := (cutoff + uint64(time.Second) - 1) / uint64(time.Second)
			// The preceding tick may already be issued but start only at recovery.
			// Reserve one due slot for it, never skipping an extra future slot.
			if hits > 0 && time.Duration(firstValid*uint64(time.Second)/uint64(p.rps)) <= elapsed {
				firstValid++
			}
			missed = firstValid - slot
		}
		p.skipped += missed
		slot += missed
		next = time.Duration(slot * uint64(time.Second) / uint64(p.rps))
	}
	if p.schedule != nil {
		p.schedule.record(hits, slot)
	}
	return max(0, next-elapsed), false
}

func (p *boundedPacer) Rate(_ time.Duration) float64 {
	return float64(p.rps)
}

func TestReadPacingSettingRejectsUnknownModesBeforeAttacks(t *testing.T) {
	for _, setting := range []struct {
		raw  string
		want pacingPolicy
	}{
		{"", pacingDiscardBacklog},
		{"discard-backlog", pacingDiscardBacklog},
		{"recent-window", pacingRecentWindow},
	} {
		t.Setenv("LOAD_READ_PACING", setting.raw)
		if got, err := readPacingSetting(); err != nil || got != setting.want {
			t.Fatalf("read pacing %q returned %q, %v", setting.raw, got, err)
		}
	}
	for _, raw := range []string{"invalid", "RECENT-WINDOW", " recent-window"} {
		t.Setenv("LOAD_READ_PACING", raw)
		if _, err := readPacingSetting(); err == nil {
			t.Fatalf("unknown read pacing accepted: %q", raw)
		}
		// Nil deployment/report proves validation returns before Docker or traffic.
		if err := runSuite(t.Context(), nil, nil, nil, t.TempDir(), nil); err == nil {
			t.Fatal("suite did not reject invalid pacing before setup")
		}
	}
}

func TestReadPacingExperimentDoesNotChangeDefaultFlagAttackPolicy(t *testing.T) {
	t.Setenv("LOAD_READ_PACING", "recent-window")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	for _, selected := range []pacingPolicy{"", pacingRecentWindow} {
		report := attack(t.Context(), attackOptions{
			pacing:   selected,
			rps:      20,
			duration: 50 * time.Millisecond,
			workers:  2,
			target:   readTarget(server.URL, "/"),
			validate: validateRead(false),
		})
		want := selected
		if want == "" {
			want = pacingDiscardBacklog
		}
		if report.PacingPolicy != want || report.CatchupBudgetMS != 10 || report.Requests == 0 || report.Failures != 0 {
			t.Fatalf("policy did not require explicit per-attack opt-in: %+v", report)
		}
	}
}

func TestBoundedPacerPreservesCadenceAndSmallTimerJitter(t *testing.T) {
	pacer := newBoundedPacer(40, 10*time.Millisecond)
	for _, sample := range []struct {
		elapsed time.Duration
		hits    uint64
		wait    time.Duration
	}{
		{
			elapsed: 0,
			hits:    0,
			wait:    25 * time.Millisecond,
		},
		{
			elapsed: 26 * time.Millisecond,
			hits:    1,
			wait:    24 * time.Millisecond,
		},
		{
			elapsed: 51 * time.Millisecond,
			hits:    2,
			wait:    24 * time.Millisecond,
		},
	} {
		wait, stop := pacer.Pace(sample.elapsed, sample.hits)
		if stop || wait != sample.wait || pacer.skipped != 0 {
			t.Fatalf("pace at %s/%d = %s, stop=%t, skipped=%d; want %s without loss", sample.elapsed, sample.hits, wait, stop, pacer.skipped, sample.wait)
		}
	}

	fast := newBoundedPacer(10000, 10*time.Millisecond)
	for hits := uint64(1); hits < 10; hits++ {
		wait, stop := fast.Pace(time.Millisecond, hits)
		if stop || wait != 0 || fast.skipped != 0 {
			t.Fatalf("ordinary 1ms scheduler wakeup could not deliver its 10k-RPS slots: wait=%s stop=%t skipped=%d", wait, stop, fast.skipped)
		}
	}
	if wait, stop := fast.Pace(time.Millisecond, 10); stop || wait != 100*time.Microsecond {
		t.Fatalf("high-rate pacing lost drift correction: %s, %t", wait, stop)
	}
}

func TestBoundedPacerDiscardsStalledBacklog(t *testing.T) {
	pacer := newBoundedPacer(40, 10*time.Millisecond)
	// Two starts have been dispatched, but the second waited for a worker until
	// 2s. The missed 75ms..2s slots must not be replayed immediately afterwards.
	wait, stop := pacer.Pace(2*time.Second, 2)
	if stop || wait != 25*time.Millisecond || pacer.skipped != 78 {
		t.Fatalf("stalled pace = %s, stop=%t, skipped=%d; want 25ms, false, 78", wait, stop, pacer.skipped)
	}
	wait, stop = pacer.Pace(2025*time.Millisecond, 3)
	if stop || wait != 25*time.Millisecond || pacer.skipped != 78 {
		t.Fatalf("discarded debt reappeared on the next start: %s, %t, %d", wait, stop, pacer.skipped)
	}
}

func TestBoundedPacerDoesNotAccumulateFractionalIntervalDrift(t *testing.T) {
	const rps = 3333
	pacer := newBoundedPacer(rps, 10*time.Millisecond)
	const elapsed = 5 * time.Hour
	hits := uint64(elapsed/time.Second) * rps
	wait, stop := pacer.Pace(elapsed, hits)
	if stop || wait != time.Second/rps || pacer.skipped != 0 {
		t.Fatalf("five-hour nominal cadence drifted: wait=%s stop=%t skipped=%d", wait, stop, pacer.skipped)
	}
}

func TestRecentPacerBudgetBoundaryAndPendingTickReservation(t *testing.T) {
	const budget = 10 * time.Millisecond
	for _, rate := range []int{40, 3333, 10000} {
		for _, delta := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
			t.Run(strconv.Itoa(rate)+"/"+delta.String(), func(t *testing.T) {
				pacer := newBoundedPacer(rate, budget)
				pacer.policy = pacingRecentWindow
				pacer.schedule = newStartSchedule()
				slot := uint64(5*time.Hour/time.Second)*uint64(rate) + 7
				due := time.Duration(slot * uint64(time.Second) / uint64(rate))
				elapsed := due + budget + delta
				wantSkipped := uint64(0)
				if delta > 0 {
					wantSkipped = 1
					if time.Duration((slot+1)*uint64(time.Second)/uint64(rate)) <= elapsed {
						wantSkipped++
					}
				}
				wait, stop := pacer.Pace(elapsed, slot-1)
				assigned, found := pacer.schedule.take(slot - 1)
				wantSlot := slot + wantSkipped
				wantWait := max(0, time.Duration(wantSlot*uint64(time.Second)/uint64(rate))-elapsed)
				if stop || !found || assigned != wantSlot || pacer.skipped != wantSkipped || wait != wantWait {
					t.Fatalf("boundary at %s: slot=%d want=%d skipped=%d want=%d wait=%s want=%s stop=%t", elapsed, assigned, wantSlot, pacer.skipped, wantSkipped, wait, wantWait, stop)
				}
			})
		}
	}
}

func TestBoundedPacerLongStallRetainsOnlyTheExistingJitterEnvelope(t *testing.T) {
	const budget = 10 * time.Millisecond
	for _, rate := range []int{40, 3333, 10000} {
		for _, epoch := range []time.Duration{0, 5 * time.Hour} {
			for _, phase := range []time.Duration{0, 123456789 * time.Nanosecond} {
				t.Run(strconv.Itoa(rate)+"/"+epoch.String()+"/"+phase.String(), func(t *testing.T) {
					elapsed := epoch + 2*time.Second + phase
					hits := uint64(epoch/time.Second) * uint64(rate)
					nominal := func(slot uint64) time.Duration {
						return time.Duration(slot * uint64(time.Second) / uint64(rate))
					}
					// Independently enumerate due times, including fractional intervals.
					firstValid := hits + 1
					for nominal(firstValid) < elapsed-budget {
						firstValid++
					}
					dueCount := uint64(0)
					for nominal(firstValid+dueCount) <= elapsed {
						dueCount++
					}
					allowedImmediate := dueCount
					pending := uint64(0)
					if hits > 0 {
						pending = 1
					}
					if hits > 0 && dueCount > 0 {
						firstValid++
						dueCount--
					}
					pacer := newBoundedPacer(rate, budget)
					pacer.policy = pacingRecentWindow
					pacer.schedule = newStartSchedule()
					// The reference starts at the first valid slot. Its lateness is
					// <=budget, so it exercises the unchanged short-jitter path.
					reference := newBoundedPacer(rate, budget)
					var actualImmediate uint64
					for issued := uint64(0); issued <= dueCount; issued++ {
						wait, stop := pacer.Pace(elapsed, hits+issued)
						allowedWait, allowedStop := reference.Pace(elapsed, firstValid-1+issued)
						assigned, found := pacer.schedule.take(hits + issued)
						if stop || allowedStop || !found || assigned != firstValid+issued || wait != allowedWait || reference.skipped != 0 || reference.discardEvents != 0 {
							t.Fatalf("stall exceeded the existing jitter envelope: assigned=%d want=%d wait=%s allowed=%s", assigned, firstValid+issued, wait, allowedWait)
						}
						if nominal(assigned) < elapsed-budget || (wait == 0) != (issued < dueCount) {
							t.Fatal("stale slot survived or catchup extended beyond currently due slots")
						}
						if wait == 0 {
							actualImmediate++
						}
					}
					if pending+actualImmediate > max(pending, allowedImmediate) {
						t.Fatal("pending tick plus retained due slots exceeded the short-jitter envelope")
					}
					if pacer.skipped != firstValid-hits-1 || pacer.discardEvents != 1 {
						t.Fatalf("lost discard accounting: skipped=%d events=%d", pacer.skipped, pacer.discardEvents)
					}
				})
			}
		}
	}
}

func TestRecentPacerDoesNotReserveAnExtraFutureSlot(t *testing.T) {
	pacer := newBoundedPacer(40, 10*time.Millisecond)
	pacer.policy = pacingRecentWindow
	// No due slot exists in [2.001s,2.011s]; slot81 at 2.025s must survive.
	wait, stop := pacer.Pace(2011*time.Millisecond, 2)
	if stop || wait != 14*time.Millisecond || pacer.skipped != 78 {
		t.Fatalf("pending reservation skipped a future slot: wait=%s skipped=%d stop=%t", wait, pacer.skipped, stop)
	}
}

func TestRecentPacerPendingTickFitsRecoveryArrivalDensity(t *testing.T) {
	for _, epoch := range []time.Duration{0, 5 * time.Hour} {
		t.Run(epoch.String(), func(t *testing.T) {
			const rate = 100
			const budget = 10 * time.Millisecond
			pacer := newBoundedPacer(rate, budget)
			pacer.policy = pacingRecentWindow
			pacer.schedule = newStartSchedule()
			baseSlot := uint64(epoch/time.Second) * rate
			// Vegeta issued slot2 (20ms) before its worker became available.
			// It starts after recovery at 130ms; the first server arrival is
			// slightly later than subsequent scheduled arrivals' network delay.
			elapsed := epoch + 130*time.Millisecond
			pendingArrival := elapsed + 200*time.Microsecond
			arrivals := []time.Duration{pendingArrival}
			for issued := uint64(0); issued < 5; issued++ {
				hits := baseSlot + 2 + issued
				wait, stop := pacer.Pace(elapsed, hits)
				assigned, found := pacer.schedule.take(hits)
				if stop || !found || assigned != baseSlot+13+issued {
					t.Fatalf("pending recovery changed the retained timeline: slot=%d wait=%s", assigned, wait)
				}
				arrival := elapsed + wait + 100*time.Microsecond
				if issued == 0 {
					arrival = max(arrival, pendingArrival+100*time.Microsecond)
				}
				arrivals = append(arrivals, arrival)
				elapsed = arrival
			}
			var inWindow int
			for _, arrival := range arrivals {
				if arrival-pendingArrival < 20*time.Millisecond {
					inWindow++
				}
			}
			if inWindow != 4 || pacer.skipped != 10 {
				t.Fatalf("pending tick recovery exceeded 4 arrivals/20ms: count=%d skipped=%d", inWindow, pacer.skipped)
			}
			// Without reserving slot12 at120ms, both120ms and130ms are retained:
			// pending130.2, catchup130.3/130.4, scheduled140.1/150.1 => five.
			unreserved := []time.Duration{pendingArrival, pendingArrival + 100*time.Microsecond,
				pendingArrival + 200*time.Microsecond, epoch + 140100*time.Microsecond, epoch + 150100*time.Microsecond}
			var unreservedCount int
			for _, arrival := range unreserved {
				if arrival-pendingArrival < 20*time.Millisecond {
					unreservedCount++
				}
			}
			if unreservedCount != 5 {
				t.Fatal("pending-tick fixture did not distinguish an unreserved recent window")
			}
		})
	}
}

func TestAttackDoesNotReplayBacklogAfterWorkerStall(t *testing.T) {
	checkWorkerStallRecovery(t, pacingDiscardBacklog)
}

func TestAttackRecentWindowDoesNotReplayStaleBacklogAfterWorkerStall(t *testing.T) {
	checkWorkerStallRecovery(t, pacingRecentWindow)
}

func checkWorkerStallRecovery(t *testing.T, policy pacingPolicy) {
	t.Helper()
	var mu sync.Mutex
	var arrivals []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		first := len(arrivals) == 1
		mu.Unlock()
		if first && !waitContext(r.Context(), 120*time.Millisecond) {
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	report := attack(t.Context(), attackOptions{
		pacing:   policy,
		rps:      100,
		duration: 300 * time.Millisecond,
		workers:  1,
		target:   readTarget(server.URL, "/"),
		validate: validateRead(false),
	})
	if report.SkippedSlots == 0 || report.GeneratorValid || report.SLOPassed {
		t.Fatalf("discarded start slots were hidden as a valid attack: %+v", report)
	}
	if report.PacingPolicy != policy || report.CatchupBudgetMS != 10 {
		t.Fatalf("recovery changed the selected policy or budget: %+v", report)
	}
	if report.Failures != 0 {
		t.Fatalf("the successful test server was reported as failing: %+v", report)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(arrivals) < 3 {
		t.Fatalf("worker did not recover after the stall: %d arrivals", len(arrivals))
	}
	var compressed int
	for _, arrival := range arrivals[1:] {
		if arrival.Sub(arrivals[1]) < 20*time.Millisecond {
			compressed++
		}
	}
	if compressed > 4 {
		t.Fatalf("worker recovery replayed a stale burst: %d requests in 20ms", compressed)
	}
}

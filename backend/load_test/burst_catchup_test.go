package load_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFlagBurstCatchupBudgetPreservesOrdinaryPacing(t *testing.T) {
	if got := flagCatchupBudget(stage{
		Name:    "flag-burst",
		FlagRPS: 400,
	}); got != 50*time.Millisecond {
		t.Fatalf("burst catchup=%s, want 50ms", got)
	}
	for _, name := range []string{"warmup", "steady-01", "recovery", "step-10000"} {
		for _, rate := range []int{2, 20, 40, 400, 1000} {
			want := min(10*time.Millisecond, time.Second/time.Duration(rate))
			if got := flagCatchupBudget(stage{
				Name:    name,
				FlagRPS: rate,
			}); got != want {
				t.Fatalf("ordinary stage=%s rate=%d catchup=%s, want unchanged %s", name, rate, got, want)
			}
		}
	}
}

func TestFlagBurstEightMillisecondWakeJitterHasBoundedCatchup(t *testing.T) {
	pacer := newBoundedPacer(400, flagCatchupBudget(stage{
		Name:    "flag-burst",
		FlagRPS: 400,
	}))
	// Slot four was due at 10ms; wake 8ms late. At 400/s, only four slots
	// are due through 18ms. Ordinary scheduling continues at 20ms.
	var immediate int
	for hits := uint64(3); hits < 8; hits++ {
		wait, stop := pacer.Pace(18*time.Millisecond, hits)
		if stop || pacer.skipped != 0 {
			t.Fatal("bounded wake jitter discarded slots or stopped the producer")
		}
		if wait == 0 {
			immediate++
			continue
		}
		if immediate != 4 || wait != 2*time.Millisecond {
			t.Fatalf("catchup=%d immediate starts then %s; want 4 then 2ms", immediate, wait)
		}
		if pacer.discardEvents != 0 || pacer.maxLateness != 8*time.Millisecond {
			t.Fatal("bounded wake jitter diagnostics changed")
		}
		return
	}
	t.Fatal("catchup did not return to its original rate timeline")
}

func TestFlagBurstHundredMillisecondStallDiscardsBacklog(t *testing.T) {
	pacer := newBoundedPacer(400, flagCatchupBudget(stage{
		Name:    "flag-burst",
		FlagRPS: 400,
	}))
	elapsed := 110 * time.Millisecond
	for hits := uint64(3); hits < 103; hits++ {
		wait, stop := pacer.Pace(elapsed, hits)
		if stop || wait != 2500*time.Microsecond || pacer.skipped != 41 {
			t.Fatalf("stall recovery hits=%d wait=%s skipped=%d stop=%t; want spaced starts and 41 discarded slots", hits, wait, pacer.skipped, stop)
		}
		if pacer.discardEvents != 1 || pacer.maxLateness != 100*time.Millisecond {
			t.Fatal("stall diagnostics lost the discard event or maximum lateness")
		}
		elapsed += wait
	}
}

func TestFlagBurstFortyMillisecondJitterCatchesOnlyDueSlots(t *testing.T) {
	pacer := newBoundedPacer(400, flagCatchupBudget(stage{
		Name:    "flag-burst",
		FlagRPS: 400,
	}))
	// Slot four was due at 10ms. Waking at 50ms permits the delayed original
	// plus 16 catchup starts; equality at the latest due slot remains inclusive.
	var immediate int
	for hits := uint64(3); hits < 22; hits++ {
		wait, stop := pacer.Pace(50*time.Millisecond, hits)
		if stop || pacer.skipped != 0 || pacer.discardEvents != 0 {
			t.Fatal("bounded 40ms wake jitter discarded slots or stopped the producer")
		}
		if wait == 0 {
			immediate++
			continue
		}
		if immediate-1 != 16 || wait != 2500*time.Microsecond || pacer.maxLateness != 40*time.Millisecond {
			t.Fatalf("catchup=%d additional starts then %s; want 16 then 2.5ms", immediate-1, wait)
		}
		return
	}
	t.Fatal("40ms catchup did not resume the original rate timeline")
}

func TestAttackReportsActualCatchupBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	for _, budget := range []time.Duration{0, 2500 * time.Microsecond, 10 * time.Millisecond, 50 * time.Millisecond} {
		report := attack(t.Context(), attackOptions{
			rps:           100,
			duration:      50 * time.Millisecond,
			workers:       2,
			target:        readTarget(server.URL, "/"),
			validate:      validateRead(false),
			catchupBudget: budget,
		})
		want := budget
		if want == 0 {
			want = 10 * time.Millisecond
		}
		if report.CatchupBudgetMS != milliseconds(want) {
			t.Fatalf("reported catchup budget=%gms, want actual %s", report.CatchupBudgetMS, want)
		}
	}
}

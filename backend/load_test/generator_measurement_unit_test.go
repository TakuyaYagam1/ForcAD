package load_test

import (
	"context"
	"math"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Feed the same recorded slots and actual start timestamps used by attack().
// These deterministic schedules isolate delivery, lateness, and skipped debt;
// successful response latency is deliberately not used to redefine a start.
func measureSyntheticStarts(
	t *testing.T,
	duration time.Duration,
	rps int,
	slots []uint64,
	actual []time.Duration,
) attackReport {
	t.Helper()
	if len(slots) != len(actual) {
		t.Fatal("slot/start fixture length mismatch")
	}
	started := time.Unix(0, 0)
	schedule := newStartSchedule()
	measurement := newGeneratorMeasurement(started, rps, newStartDensity(duration), schedule)
	span := duration
	for index, slot := range slots {
		schedule.record(uint64(index), slot)
		measurement.add(uint64(index), started.Add(actual[index]))
		span = max(span, actual[index])
	}
	report := attackReport{
		OfferedRPS:  rps,
		Requests:    uint64(len(slots)),
		AchievedRPS: float64(len(slots)) / span.Seconds(),
	}
	measurement.populate(context.Background(), &report, schedule.clear())
	return report
}

func startsWithDroppedRange(firstDropped, countDropped int) (slots []uint64, actual []time.Duration) {
	for slot := 1; slot <= 1000; slot++ {
		if slot >= firstDropped && slot < firstDropped+countDropped {
			continue
		}
		slots = append(slots, uint64(slot))
		actual = append(actual, time.Duration(slot)*10*time.Millisecond)
	}
	return slots, actual
}

func TestGeneratorMeasurementRejects94PercentOnTimeDelivery(t *testing.T) {
	slots, actual := startsWithDroppedRange(201, 60)
	report := measureSyntheticStarts(t, 10*time.Second, 100, slots, actual)
	if report.GeneratorValid || report.ScheduledLatenessP99MS != 0 || math.Abs(report.DeliveryRatio-.94) > 1e-12 {
		t.Fatalf("94%% on-time delivery was hidden as valid: %+v", report)
	}
}

func TestGeneratorMeasurementSeparates98PercentDeliveryFromLegacyDebt(t *testing.T) {
	slots, actual := startsWithDroppedRange(201, 20)
	report := measureSyntheticStarts(t, 10*time.Second, 100, slots, actual)
	if !report.GeneratorValid || report.ScheduledLatenessP99MS != 0 || math.Abs(report.DeliveryRatio-.98) > 1e-12 {
		t.Fatalf("recovered 98%% schedule was measured incorrectly: %+v", report)
	}
	if report.MetricVersion != 2 || report.LagSemantics != "legacy_offered_schedule_debt" || report.LagP99MS < 199 {
		t.Fatalf("legacy 200ms skipped debt or version was lost: %+v", report)
	}
}

func TestGeneratorMeasurementRejectsLateBurstDespite100PercentDelivery(t *testing.T) {
	slots, actual := startsWithDroppedRange(1001, 0)
	for index := 20; index < 50; index++ {
		actual[index] += 200 * time.Millisecond
	}
	// Recover by starting the next20 requests together. Sequence timestamps stay
	// ordered, and the final request still starts at10s: delivery is exactly100%.
	for index := 50; index < 70; index++ {
		actual[index] = 700*time.Millisecond + time.Duration(index-49)*time.Microsecond
	}
	report := measureSyntheticStarts(t, 10*time.Second, 100, slots, actual)
	if report.GeneratorValid || report.DeliveryRatio != 1 || report.ScheduledLatenessP99MS < 199 {
		t.Fatalf("late starts were hidden by full delivery: %+v", report)
	}
	if report.MaxStarts1MS < 20 || report.ScheduledLatenessMaxMS < 200 {
		t.Fatalf("recovery burst or actual lateness was lost: %+v", report)
	}
}

func TestGeneratorMeasurementKeepsOldLateSlotAfterNewSkipsAndOutOfOrderResponses(t *testing.T) {
	started := time.Unix(0, 0)
	schedule := newStartSchedule()
	pacer := newBoundedPacer(100, 10*time.Millisecond)
	pacer.schedule = schedule
	_, _ = pacer.Pace(0, 0) // Seq0 receives absolute slot1 (10ms).
	_, _ = pacer.Pace(400*time.Millisecond, 1)
	if pacer.skipped != 39 {
		t.Fatalf("fixture did not skip the expected slots: %d", pacer.skipped)
	}
	measurement := newGeneratorMeasurement(started, 100, newStartDensity(time.Second), schedule)
	// Later response returns first; Seq0 remains pending after the new skips.
	measurement.add(1, started.Add(411*time.Millisecond)) // Slot41, lateness1ms.
	measurement.add(0, started.Add(200*time.Millisecond)) // Slot1, lateness190ms.
	report := attackReport{
		OfferedRPS:  100,
		AchievedRPS: 100,
	}
	measurement.populate(context.Background(), &report, schedule.clear())
	if report.ScheduledLatenessMaxMS != 190 || report.LagMaxMS != 391 || report.ScheduleErrors != 0 {
		t.Fatalf("current skips rewrote an older request's schedule: %+v", report)
	}
}

func TestGeneratorMeasurementMissingCorrelationFailsClosed(t *testing.T) {
	started := time.Unix(0, 0)
	schedule := newStartSchedule()
	measurement := newGeneratorMeasurement(started, 100, newStartDensity(time.Second), schedule)
	measurement.add(0, started.Add(10*time.Millisecond))
	report := attackReport{
		OfferedRPS:  100,
		AchievedRPS: 100,
	}
	measurement.populate(context.Background(), &report, schedule.clear())
	if report.GeneratorValid || report.ScheduleErrors != 1 {
		t.Fatalf("missing correlation produced a valid attack: %+v", report)
	}
	if report.ScheduledSamples != 0 || report.ScheduledLatenessP99MS != 0 || report.ScheduledLatenessP999MS != 0 {
		t.Fatalf("empty scheduled digest produced invalid diagnostics: %+v", report)
	}
}

func TestGeneratorMeasurementCancellationStillInvalidatesOnTimeDelivery(t *testing.T) {
	started := time.Unix(0, 0)
	schedule := newStartSchedule()
	measurement := newGeneratorMeasurement(started, 100, newStartDensity(time.Second), schedule)
	schedule.record(0, 1)
	measurement.add(0, started.Add(10*time.Millisecond))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report := attackReport{
		OfferedRPS:  100,
		AchievedRPS: 100,
	}
	measurement.populate(ctx, &report, schedule.clear())
	if report.GeneratorValid {
		t.Fatal("canceled generator was marked valid")
	}
}

func TestStartScheduleReclaimsOutOfOrderResultsAndUndispatchedTail(t *testing.T) {
	const workers = 4
	schedule := newStartSchedule()
	inflight := make([]uint64, workers)
	for sequence := range uint64(workers) {
		inflight[sequence] = sequence
		schedule.record(sequence, sequence+1)
	}
	for sequence := uint64(workers); sequence < 10000; sequence++ {
		// One planned tick can coexist with all busy workers. Complete an older
		// response out of order, then dispatch that already-recorded tick.
		schedule.record(sequence, sequence+1)
		index := int(sequence % workers)
		old := inflight[index]
		if slot, ok := schedule.take(old); !ok || slot != old+1 {
			t.Fatalf("lost sequence %d while reclaiming out of order", old)
		}
		inflight[index] = sequence
	}
	for _, sequence := range inflight {
		_, _ = schedule.take(sequence)
	}
	schedule.record(10000, 10001) // Stop occurs before this tick is dispatched.
	if pending := schedule.clear(); pending != 1 {
		t.Fatalf("undispatched tail=%d, want 1", pending)
	}
	if remaining := schedule.clear(); remaining != 0 {
		t.Fatalf("clear retained %d old entries", remaining)
	}
	if peak, failures := schedule.diagnostics(); peak != workers+1 || failures != 0 {
		t.Fatalf("unbounded retention: peak=%d errors=%d", peak, failures)
	}
}

func TestStartScheduleConcurrentRecordAndCompletion(t *testing.T) {
	const workers = 4
	schedule := newStartSchedule()
	jobs := make(chan uint64)
	results := make(chan uint64)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for sequence := range jobs {
				runtime.Gosched()
				results <- sequence
			}
		})
	}
	go func() {
		for sequence := range uint64(1000) {
			schedule.record(sequence, sequence+1)
			jobs <- sequence
		}
		close(jobs)
		group.Wait()
		close(results)
	}()
	for sequence := range results {
		runtime.Gosched()
		if slot, ok := schedule.take(sequence); !ok || slot != sequence+1 {
			t.Errorf("lost sequence %d during concurrent completion", sequence)
		}
	}
	if remaining := schedule.clear(); remaining != 0 {
		t.Fatalf("completed attack retained %d entries", remaining)
	}
	if peak, failures := schedule.diagnostics(); peak > workers+2 || failures != 0 {
		t.Fatalf("concurrent correlation: peak=%d errors=%d", peak, failures)
	}
}

func TestStartScheduleIncludesReceivedResultHandoffInBound(t *testing.T) {
	// Real unbuffered result handoff frees the worker before slot deletion.
	schedule := newStartSchedule()
	jobs := make(chan uint64)
	results := make(chan uint64)
	plannedThird := make(chan struct{})
	go func() {
		defer close(results)
		for sequence := range jobs {
			results <- sequence
		}
	}()
	go func() {
		defer close(jobs)
		for sequence := range uint64(3) {
			schedule.record(sequence, sequence+1)
			if sequence == 2 {
				close(plannedThird)
			}
			jobs <- sequence
		}
	}()
	first := <-results
	<-plannedThird
	if peak, failures := schedule.diagnostics(); peak != 3 || failures != 0 {
		t.Fatalf("worker+consumer+planned bound: peak=%d errors=%d", peak, failures)
	}
	_, _ = schedule.take(first)
	for sequence := range results {
		_, _ = schedule.take(sequence)
	}
	if remaining := schedule.clear(); remaining != 0 {
		t.Fatalf("handoff retained %d completed slots", remaining)
	}
}

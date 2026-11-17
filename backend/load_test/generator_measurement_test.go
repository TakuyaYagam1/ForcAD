package load_test

import (
	"context"
	"sync"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// startSchedule stores absolute offered-slot ordinals, not the current skipped
// count: responses can complete after further skips and in a different order.
// Vegeta's single producer records hits=n before dispatching tick n. Its result
// Seq and Timestamp are assigned together under seqmu. Matching these ordered
// streams measures nth-start lateness; it does not identify a particular worker
// tick when workers reorder before seqmu.
//
// There can be MaxWorkers active workers, one result already handed to the
// consumer but not yet deleted, and one planned tick: MaxWorkers+2 records.
// No pacing/backpressure policy is added. Delete happens before validation;
// clear runs after Attack closes. The short mutex sections have normal
// instrumentation overhead, without intentional producer waits or sleeps.
type startSchedule struct {
	mu      sync.Mutex
	pending map[uint64]uint64
	peak    uint64
	errors  uint64
}

func newStartSchedule() *startSchedule {
	return &startSchedule{
		pending: make(map[uint64]uint64),
	}
}

func (s *startSchedule) record(sequence, slot uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, duplicate := s.pending[sequence]; duplicate {
		s.errors++
		return // Keep the original slot; never hide an older late start.
	}
	s.pending[sequence] = slot
	s.peak = max(s.peak, uint64(len(s.pending)))
}

func (s *startSchedule) take(sequence uint64) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, found := s.pending[sequence]
	if found {
		delete(s.pending, sequence)
	} else {
		s.errors++
	}
	return slot, found
}

func (s *startSchedule) clear() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining := uint64(len(s.pending))
	clear(s.pending)
	return remaining
}

func (s *startSchedule) diagnostics() (peak, failures uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak, s.errors
}

type generatorMeasurement struct {
	started          time.Time
	rps              int
	schedule         *startSchedule
	legacy           vegeta.LatencyMetrics
	scheduled        vegeta.LatencyMetrics
	legacyMax        time.Duration
	scheduledMax     time.Duration
	scheduledSamples uint64
	starts           *startDensity
}

func newGeneratorMeasurement(
	started time.Time,
	rps int,
	starts *startDensity,
	schedule *startSchedule,
) *generatorMeasurement {
	return &generatorMeasurement{
		started:  started,
		rps:      rps,
		schedule: schedule,
		starts:   starts,
	}
}

func (m *generatorMeasurement) add(sequence uint64, timestamp time.Time) {
	slot, found := m.schedule.take(sequence)
	elapsed := timestamp.Sub(m.started)
	// Preserve the pre-v2 metric and its floating-point formula exactly. This
	// includes skipped-slot debt and is not a latency of an individual stall.
	legacy := max(0, elapsed-time.Duration(float64(sequence+1)*float64(time.Second)/float64(m.rps)))
	m.legacy.Add(legacy)
	m.legacyMax = max(m.legacyMax, legacy)
	m.starts.add(elapsed)
	if !found {
		return // Missing correlation fails GeneratorValid; never invent a slot.
	}
	// Supported <=12h and <=100kRPS keep slot*1s within uint64/int64 limits.
	due := time.Duration(slot * uint64(time.Second) / uint64(m.rps))
	late := max(0, elapsed-due)
	m.scheduled.Add(late)
	m.scheduledMax = max(m.scheduledMax, late)
	m.scheduledSamples++
}

func (m *generatorMeasurement) populate(ctx context.Context, report *attackReport, unissued uint64) {
	report.MetricVersion = 2
	report.LagSemantics = "legacy_offered_schedule_debt"
	report.LagP99MS = milliseconds(m.legacy.Quantile(.99))
	report.LagP999MS = milliseconds(m.legacy.Quantile(.999))
	report.LagMaxMS = milliseconds(m.legacyMax)
	var scheduledP99 time.Duration
	if m.scheduledSamples > 0 {
		scheduledP99 = m.scheduled.Quantile(.99)
		report.ScheduledLatenessP999MS = milliseconds(m.scheduled.Quantile(.999))
	}
	report.ScheduledSamples = m.scheduledSamples
	report.ScheduledLatenessP99MS = milliseconds(scheduledP99)
	report.ScheduledLatenessMaxMS = milliseconds(m.scheduledMax)
	report.MaxStarts1MS, report.MaxStarts10MS = m.starts.peaks()
	report.SchedulePendingPeak, report.ScheduleErrors = m.schedule.diagnostics()
	report.ScheduleUnissuedSlots = unissued
	if report.OfferedRPS > 0 {
		report.DeliveryRatio = report.AchievedRPS / float64(report.OfferedRPS)
	}
	// This intentionally versions the measurement policy. Keep the existing
	// 95% delivery and 100ms timing thresholds, applying the timing threshold
	// to recorded scheduled slots rather than cumulative skipped-slot debt.
	report.GeneratorValid = report.DeliveryRatio >= minimumDelivery &&
		scheduledP99 <= generatorLagBudget && ctx.Err() == nil &&
		report.ScheduleErrors == 0 && m.scheduledSamples == report.Requests && unissued <= 1
}

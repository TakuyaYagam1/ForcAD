package load_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	readWorkers     = 1024
	flagWorkers     = 64
	responseLimit   = 1 << 20 // Include attack_data for 28 teams and 40 PFR places.
	minimumDelivery = 0.95
)

type attackReport struct {
	PacingPolicy                   pacingPolicy           `json:"generator_pacing_policy"`
	HTTPIngressTransport           string                 `json:"http_ingress_transport"`
	TransportErrorsTotal           uint64                 `json:"transport_errors_total"`
	TransportErrorSamplesTruncated uint64                 `json:"transport_error_samples_truncated"`
	TransportErrorSamples          []transportErrorSample `json:"transport_error_samples,omitempty"`
	WorkerLimit                    uint64                 `json:"generator_worker_limit"`
	InitialWorkers                 uint64                 `json:"generator_initial_workers"`
	OfferedRPS                     int                    `json:"offered_rps"`
	AchievedRPS                    float64                `json:"achieved_rps"`
	StartedUTC                     *time.Time             `json:"started_utc,omitempty"`
	LastRequestStartUTC            *time.Time             `json:"last_request_start_utc,omitempty"`
	DrainedUTC                     *time.Time             `json:"drained_utc,omitempty"`
	MonotonicElapsedToDrainSeconds float64                `json:"monotonic_elapsed_to_drain_seconds,omitempty"`
	LinuxClock                     attackLinuxClock       `json:"linux_clock"`
	PostprocessElapsedSeconds      float64                `json:"postprocess_elapsed_seconds,omitempty"`
	RateSpanSeconds                float64                `json:"rate_span_seconds,omitempty"`
	Requests                       uint64                 `json:"requests"`
	ResponseBodyBytesTotal         uint64                 `json:"response_body_bytes_total"`
	ResponseBodyBytesMean          float64                `json:"response_body_bytes_mean"`
	MeanMS                         float64                `json:"mean_ms"`
	ResultConsumerDelayMeanMS      float64                `json:"result_consumer_delay_mean_ms"`
	ResultConsumerDelayMaxMS       float64                `json:"result_consumer_delay_max_ms"`
	ResultConsumerDelaySamples     uint64                 `json:"result_consumer_delay_samples"`
	Statuses                       map[string]int         `json:"statuses"`
	Rejections                     map[string]int         `json:"rejections,omitempty"`
	P95MS                          float64                `json:"p95_ms"`
	P99MS                          float64                `json:"p99_ms"`
	LagP99MS                       float64                `json:"generator_lag_p99_ms"`
	LagP999MS                      float64                `json:"generator_lag_p999_ms"`
	LagMaxMS                       float64                `json:"generator_lag_max_ms"`
	MetricVersion                  int                    `json:"generator_metric_version"`
	LagSemantics                   string                 `json:"generator_lag_semantics"`
	ScheduledLatenessP99MS         float64                `json:"generator_scheduled_lateness_p99_ms"`
	ScheduledLatenessP999MS        float64                `json:"generator_scheduled_lateness_p999_ms"`
	ScheduledLatenessMaxMS         float64                `json:"generator_scheduled_lateness_max_ms"`
	DeliveryRatio                  float64                `json:"generator_delivery_ratio"`
	ScheduleErrors                 uint64                 `json:"generator_schedule_errors"`
	ScheduledSamples               uint64                 `json:"generator_scheduled_samples"`
	SchedulePendingPeak            uint64                 `json:"generator_schedule_pending_peak"`
	ScheduleUnissuedSlots          uint64                 `json:"generator_schedule_unissued_slots"`
	MaxStarts1MS                   uint64                 `json:"generator_max_starts_fixed_1ms"`
	MaxStarts10MS                  uint64                 `json:"generator_max_starts_fixed_10ms"`
	SkippedSlots                   uint64                 `json:"generator_skipped_slots"`
	CatchupBudgetMS                float64                `json:"generator_catchup_budget_ms,omitempty"`
	DiscardEvents                  uint64                 `json:"generator_discard_events"`
	MaxPacerLatenessMS             float64                `json:"generator_max_pacer_lateness_ms"`
	GeneratorValid                 bool                   `json:"generator_valid"`
	SLOPassed                      bool                   `json:"slo_passed"`
	Failures                       uint64                 `json:"failures"`
	FirstError                     string                 `json:"first_error,omitempty"`
	TransportPhases                *transportPhaseReport  `json:"transport_phases,omitempty"`
}

type attackOptions struct {
	pacing        pacingPolicy
	ingress       *httpIngressRoute
	rps           int
	duration      time.Duration
	workers       uint64
	target        vegeta.Targeter
	validate      func(*vegeta.Result) error
	catchupBudget time.Duration
}

func attack(ctx context.Context, options attackOptions) attackReport {
	transport := &http.Transport{
		MaxIdleConns:        int(options.workers),
		MaxIdleConnsPerHost: int(options.workers),
		MaxConnsPerHost:     int(options.workers),
		IdleConnTimeout:     10 * time.Second,
	}
	if options.ingress != nil && options.ingress.metadata.Transport == "direct" {
		transport.DialContext = options.ingress.dialContext
	}
	client := &http.Client{
		Timeout:   requestTimeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	var phases *transportPhaseTransport
	if os.Getenv("LOAD_HTTP_PHASES") == "1" {
		phases = newTransportPhaseTransport(client.Transport)
		client.Transport = phases
	}
	defer client.CloseIdleConnections()
	attacker := vegeta.NewAttacker(vegeta.Client(client), vegeta.Workers(min(options.workers, 32)),
		vegeta.MaxWorkers(options.workers), vegeta.MaxBody(responseLimit))
	stop := context.AfterFunc(ctx, func() {
		attacker.Stop()
	})
	defer stop()
	var metrics vegeta.Metrics
	starts := newStartDensity(options.duration)
	report := attackReport{
		HTTPIngressTransport: "published",
		OfferedRPS:           options.rps,
		Rejections:           make(map[string]int),
		WorkerLimit:          options.workers,
		InitialWorkers:       min(options.workers, 32),
	}
	if options.ingress != nil {
		report.HTTPIngressTransport = options.ingress.metadata.Transport
	}
	startClock, startClockErr := readAttackLinuxClock()
	started := time.Now()
	pacer := newBoundedPacer(options.rps, options.catchupBudget)
	if options.pacing == pacingRecentWindow {
		pacer.policy = pacingRecentWindow
	}
	report.PacingPolicy = pacer.policy
	pacer.schedule = newStartSchedule()
	measurement := newGeneratorMeasurement(started, options.rps, starts, pacer.schedule)
	var consumerDelayTotal, consumerDelayMax time.Duration
	for result := range attacker.Attack(options.target, pacer, options.duration, "load") {
		if delay, valid := resultConsumerDelay(result, time.Now()); valid {
			consumerDelayTotal += delay
			consumerDelayMax = max(consumerDelayMax, delay)
			report.ResultConsumerDelaySamples++
		}
		measurement.add(result.Seq, result.Timestamp)
		if reason := rejectionReason(result); reason != "" {
			report.Rejections[reason]++
		}
		if err := options.validate(result); err != nil {
			report.Failures++
			if report.FirstError == "" {
				report.FirstError = err.Error()
			}
		}
		// URLs include request correlation; collecting every transport error string
		// would retain an unbounded set during a long outage.
		report.recordTransportError(result)
		result.Error = ""
		metrics.Add(result)
	}
	drained := time.Now()
	drainClock, drainClockErr := readAttackLinuxClock()
	report.LinuxClock = attackLinuxClock{
		StartBeforeNS:      startClock.monotonicNS,
		DrainAfterNS:       drainClock.monotonicNS,
		BootID:             startClock.bootID,
		DrainBootID:        drainClock.bootID,
		TimeNamespace:      startClock.timeNamespace,
		DrainTimeNamespace: drainClock.timeNamespace,
	}
	if err := errors.Join(startClockErr, drainClockErr); err != nil {
		report.LinuxClock.Error = err.Error()
	}
	// Attack closes only after its producer and every worker have stopped. A
	// stop during Sleep/tick handoff can leave one recorded, undispatched slot.
	unissued := pacer.schedule.clear()
	metrics.Close()
	report.Requests, report.Statuses = metrics.Requests, metrics.StatusCodes
	report.ResponseBodyBytesTotal = metrics.BytesIn.Total
	report.ResponseBodyBytesMean = metrics.BytesIn.Mean
	report.MeanMS = milliseconds(metrics.Latencies.Mean)
	if report.ResultConsumerDelaySamples > 0 {
		report.ResultConsumerDelayMeanMS = milliseconds(consumerDelayTotal) / float64(report.ResultConsumerDelaySamples)
		report.ResultConsumerDelayMaxMS = milliseconds(consumerDelayMax)
	}
	span := max(options.duration, metrics.Latest.Sub(started))
	report.AchievedRPS = float64(metrics.Requests) / span.Seconds()
	report.P95MS, report.P99MS = milliseconds(metrics.Latencies.P95), milliseconds(metrics.Latencies.P99)
	measurement.populate(ctx, &report, unissued)
	report.SkippedSlots = pacer.skipped
	report.CatchupBudgetMS = milliseconds(pacer.catchupBudget)
	report.DiscardEvents = pacer.discardEvents
	report.MaxPacerLatenessMS = milliseconds(pacer.maxLateness)
	report.SLOPassed = report.GeneratorValid && report.Failures == 0 && metrics.Success == 1 && metrics.Latencies.P99 <= latencyBudget
	if phases != nil {
		report.TransportPhases = phases.snapshot()
	}
	startedUTC, drainedUTC := started.UTC(), drained.UTC()
	report.StartedUTC, report.DrainedUTC = &startedUTC, &drainedUTC
	if metrics.Requests > 0 {
		lastRequestStartUTC := metrics.Latest.UTC()
		report.LastRequestStartUTC = &lastRequestStartUTC
	}
	report.MonotonicElapsedToDrainSeconds = drained.Sub(started).Seconds()
	report.RateSpanSeconds = span.Seconds()
	report.PostprocessElapsedSeconds = time.Since(drained).Seconds()
	return report
}

// Count starts by Vegeta's timestamps, even when responses finish out of order.
// Buckets are aligned to the attack start, not sliding windows or wire arrivals.
// Allocation happens before measurement and costs about 4 KiB per scheduled second.
type startDensity struct {
	buckets []uint32
}

func newStartDensity(duration time.Duration) *startDensity {
	return &startDensity{
		buckets: make([]uint32, int(duration/time.Millisecond)+1),
	}
}

func (d *startDensity) add(elapsed time.Duration) {
	index := int(max(0, elapsed) / time.Millisecond)
	if index >= len(d.buckets) {
		// A worker may start the final request after the nominal stage duration.
		d.buckets = append(d.buckets, make([]uint32, index-len(d.buckets)+1)...)
	}
	d.buckets[index]++
}

func (d *startDensity) peaks() (oneMS, tenMS uint64) {
	var count uint64
	for index, requests := range d.buckets {
		if index%10 == 0 {
			count = 0
		}
		oneMS = max(oneMS, uint64(requests))
		count += uint64(requests)
		tenMS = max(tenMS, count)
	}
	return oneMS, tenMS
}

// Record a fixed set of reasons, never arbitrary response bodies or credentials.
func rejectionReason(result *vegeta.Result) string {
	if result.Code == http.StatusTooManyRequests {
		return "rate_limit"
	}
	if result.Code != http.StatusServiceUnavailable {
		return ""
	}
	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(result.Body, &response); err != nil {
		return "other"
	}
	switch response.Error {
	case "Service capacity exceeded":
		return "capacity"
	case "Request deadline exceeded":
		return "deadline"
	case "Game is paused":
		return "paused"
	case "Service temporarily unavailable":
		return "unavailable"
	default:
		return "other"
	}
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func validateRead(overload bool) func(*vegeta.Result) error {
	return func(result *vegeta.Result) error {
		if overload && (result.Code == http.StatusTooManyRequests || result.Code == http.StatusServiceUnavailable) {
			return nil
		}
		if result.Code != http.StatusOK {
			return fmt.Errorf("load.validateRead - HTTP %d: %s", result.Code, result.Error)
		}
		if !json.Valid(result.Body) {
			return fmt.Errorf("load.validateRead - invalid or truncated JSON")
		}
		return nil
	}
}

func checkAttack(name string, report attackReport, overload bool) error {
	if report.Failures > 0 {
		return fmt.Errorf("load.checkAttack - %s: %d failures: %s", name, report.Failures, report.FirstError)
	}
	if !report.GeneratorValid {
		return fmt.Errorf("load.checkAttack - %s missed generator delivery or timing budget", name)
	}
	if !overload && !report.SLOPassed {
		return fmt.Errorf("load.checkAttack - %s missed latency, success or generator delivery budget", name)
	}
	return nil
}

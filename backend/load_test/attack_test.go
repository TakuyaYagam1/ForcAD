package load_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	readWorkers     = 1024
	flagWorkers     = 64
	responseLimit   = 128 << 10
	minimumDelivery = 0.95
)

type attackReport struct {
	OfferedRPS     int            `json:"offered_rps"`
	AchievedRPS    float64        `json:"achieved_rps"`
	Requests       uint64         `json:"requests"`
	Statuses       map[string]int `json:"statuses"`
	Rejections     map[string]int `json:"rejections,omitempty"`
	P95MS          float64        `json:"p95_ms"`
	P99MS          float64        `json:"p99_ms"`
	LagP99MS       float64        `json:"generator_lag_p99_ms"`
	GeneratorValid bool           `json:"generator_valid"`
	SLOPassed      bool           `json:"slo_passed"`
	Failures       uint64         `json:"failures"`
	FirstError     string         `json:"first_error,omitempty"`
}

type attackOptions struct {
	rps      int
	duration time.Duration
	workers  uint64
	target   vegeta.Targeter
	validate func(*vegeta.Result) error
}

func attack(ctx context.Context, options attackOptions) attackReport {
	client := &http.Client{Timeout: requestTimeout, Transport: &http.Transport{
		MaxIdleConns: int(options.workers), MaxIdleConnsPerHost: int(options.workers),
		MaxConnsPerHost: int(options.workers), IdleConnTimeout: 30 * time.Second,
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	attacker := vegeta.NewAttacker(vegeta.Client(client), vegeta.Workers(min(options.workers, 32)),
		vegeta.MaxWorkers(options.workers), vegeta.MaxBody(responseLimit))
	stop := context.AfterFunc(ctx, func() { attacker.Stop() })
	defer stop()
	var metrics vegeta.Metrics
	var lagMetrics vegeta.LatencyMetrics
	report := attackReport{OfferedRPS: options.rps, Rejections: make(map[string]int)}
	started := time.Now()
	for result := range attacker.Attack(options.target, vegeta.Rate{Freq: options.rps, Per: time.Second}, options.duration, "load") {
		lag := max(0, result.Timestamp.Sub(started)-time.Duration(float64(result.Seq+1)*float64(time.Second)/float64(options.rps)))
		lagMetrics.Add(lag)
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
		result.Error = ""
		metrics.Add(result)
	}
	metrics.Close()
	report.Requests, report.Statuses = metrics.Requests, metrics.StatusCodes
	span := max(options.duration, metrics.Latest.Sub(started))
	report.AchievedRPS = float64(metrics.Requests) / span.Seconds()
	report.P95MS, report.P99MS = milliseconds(metrics.Latencies.P95), milliseconds(metrics.Latencies.P99)
	lagP99 := lagMetrics.Quantile(0.99)
	report.LagP99MS = milliseconds(lagP99)
	report.GeneratorValid = report.AchievedRPS >= minimumDelivery*float64(options.rps) && lagP99 <= generatorLagBudget && ctx.Err() == nil
	report.SLOPassed = report.GeneratorValid && report.Failures == 0 && metrics.Success == 1 && metrics.Latencies.P99 <= latencyBudget
	return report
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
	if !overload && !report.SLOPassed {
		return fmt.Errorf("load.checkAttack - %s missed latency, success or generator delivery budget", name)
	}
	return nil
}

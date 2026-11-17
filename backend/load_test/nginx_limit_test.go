package load_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func checkSingleIPRateLimit(
	ctx context.Context,
	origin string,
	ingress *httpIngressRoute,
	logf func(string, ...any),
) error {
	const duration = 2 * time.Second
	url := origin + "/api/v1/client/status/"
	target := func(request *vegeta.Target) error {
		*request = vegeta.Target{
			Method: http.MethodGet,
			URL:    url,
		}
		return nil
	}
	for _, probe := range []struct {
		rps      int
		allow429 bool
	}{
		{
			rps: 120,
		},
		{
			rps:      300,
			allow429: true,
		},
	} {
		result := attack(ctx, attackOptions{
			ingress:  ingress,
			rps:      probe.rps,
			duration: duration,
			workers:  readWorkers,
			target:   target,
			validate: func(response *vegeta.Result) error {
				if probe.allow429 && response.Code == http.StatusTooManyRequests && response.Headers.Get("Retry-After") != "" {
					return nil
				}
				if response.Error != "" {
					return fmt.Errorf("request failed: %s", response.Error)
				}
				if response.Code == http.StatusOK {
					return nil
				}
				return fmt.Errorf("unexpected HTTP status %d", response.Code)
			},
		})
		logf("single-IP limiter: offered=%d/s actual=%.0f/s HTTP200=%d HTTP429=%d valid=%t",
			probe.rps, result.AchievedRPS, result.Statuses["200"], result.Statuses["429"], result.GeneratorValid)
		if !result.GeneratorValid || result.Failures != 0 {
			return fmt.Errorf("load.checkSingleIPRateLimit - %d RPS probe failed: %s (generator valid: %t)",
				probe.rps, result.FirstError, result.GeneratorValid)
		}
		if probe.allow429 {
			if result.Statuses["429"] == 0 || result.Statuses["200"] == 0 {
				return fmt.Errorf("load.checkSingleIPRateLimit - sustained %d RPS did not exercise both accepted and rate-limited requests", probe.rps)
			}
		} else if result.Statuses["200"] != int(result.Requests) {
			return fmt.Errorf("load.checkSingleIPRateLimit - %d RPS was limited below the configured per-IP quota", probe.rps)
		}
	}
	return nil
}

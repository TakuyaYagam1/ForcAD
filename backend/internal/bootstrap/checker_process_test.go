package bootstrap

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	checkerworker "github.com/TakuyaYagam1/ForcAD/internal/worker/checker"
	promclient "github.com/prometheus/client_golang/prometheus"
)

func TestShutdownWaitAndResourceCleanupAreBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var group sync.WaitGroup
		group.Add(1)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		started := time.Now()
		if waitGroups(ctx, &group) || time.Since(started) != time.Second {
			t.Fatal("stuck handler kept shutdown waiting beyond its deadline")
		}
		group.Done()
		synctest.Wait()
		finished := make(chan struct{})
		started = time.Now()
		if err := cleanupResources(func() { <-finished }, time.Second); err == nil || time.Since(started) != time.Second {
			t.Fatal("stuck pool cleanup prevented bounded process exit")
		}
		close(finished)
		synctest.Wait()
		if err := cleanupResources(func() {}, time.Second); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCheckerProcessMetricsStartHealthy(t *testing.T) {
	worker := checkerworker.New(nil, nil, nil, nil, checkerworker.Options{}, nil)
	registry := promclient.NewRegistry()
	registerCheckerProcessMetrics(registry, worker)
	metrics, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 4 {
		t.Fatalf("metrics = %d, want 4", len(metrics))
	}
	for _, metric := range metrics {
		for _, sample := range metric.Metric {
			if sample.GetGauge().GetValue() != 0 || sample.GetCounter().GetValue() != 0 {
				t.Fatalf("healthy metric %s was nonzero", metric.GetName())
			}
		}
	}
	if checkerFailure(worker) != nil || checkerFailures(nil) != nil {
		t.Fatal("healthy worker requested replacement")
	}
}

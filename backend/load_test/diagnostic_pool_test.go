package load_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestPoolMetricsFollowConfiguredReceiverAndAPIReplicas(t *testing.T) {
	for _, setting := range []struct {
		name, receivers, api string
		wantReceivers        int
		wantAPI              int
	}{
		{
			name:          "defaults",
			wantReceivers: 2,
			wantAPI:       2,
		},
		{
			name:          "one receiver and four API",
			receivers:     "1",
			api:           "4",
			wantReceivers: 1,
			wantAPI:       4,
		},
		{
			name:          "four receivers",
			receivers:     "4",
			wantReceivers: 4,
			wantAPI:       2,
		},
		{
			name:          "one API",
			receivers:     "4",
			api:           "1",
			wantReceivers: 4,
			wantAPI:       1,
		},
		{
			name:          "upper bounds",
			receivers:     "16",
			api:           "16",
			wantReceivers: 16,
			wantAPI:       16,
		},
	} {
		t.Run(setting.name, func(t *testing.T) {
			t.Setenv("LOAD_RECEIVER_REPLICAS", setting.receivers)
			t.Setenv("LOAD_API_REPLICAS", setting.api)
			// The load wrapper resolves LOAD_* and overwrites FORCAD_* counts.
			t.Setenv("FORCAD_RECEIVER_REPLICAS", "12")
			t.Setenv("FORCAD_API_REPLICAS", "13")
			t.Setenv("LOAD_WORKER_REPLICAS", "20")
			got, err := poolMetricReplicas()
			if err != nil {
				t.Fatal(err)
			}
			var want []poolMetricReplica
			for index := 1; index <= setting.wantReceivers; index++ {
				want = append(want, poolMetricReplica{"receiver", strconv.Itoa(index)})
			}
			for index := 1; index <= setting.wantAPI; index++ {
				want = append(want, poolMetricReplica{"api", strconv.Itoa(index)})
			}
			if !slices.Equal(got, want) {
				t.Fatalf("pool metrics replicas=%v; want %v", got, want)
			}
		})
	}
}

func TestPoolMetricsRejectInvalidReplicaCounts(t *testing.T) {
	for _, name := range []string{"LOAD_RECEIVER_REPLICAS", "LOAD_API_REPLICAS"} {
		for _, raw := range []string{"0", "17", "-1", "1.5", "invalid", "999999999999999999999"} {
			t.Run(name+"="+raw, func(t *testing.T) {
				t.Setenv("LOAD_RECEIVER_REPLICAS", "2")
				t.Setenv("LOAD_API_REPLICAS", "2")
				t.Setenv(name, raw)
				replicas, err := poolMetricReplicas()
				if err == nil || !strings.Contains(err.Error(), name) || replicas != nil {
					t.Fatalf("invalid %s=%q returned replicas=%v err=%v", name, raw, replicas, err)
				}
			})
		}
	}
}

func TestPoolMetricsRetainRuntimePressureSignalsFromExistingScrapes(t *testing.T) {
	for _, line := range []string{
		`go_gc_duration_seconds{quantile="0.99"} 0.0004`,
		`go_gc_duration_seconds_sum 0.15`,
		`go_gc_duration_seconds_count 123`,
		`go_threads 42`,
		`go_memstats_heap_alloc_bytes 123456`,
		`go_memstats_heap_objects 1234`,
		`go_memstats_heap_inuse_bytes 234567`,
		`go_gc_cpu_fraction 0.013`,
		`go_sched_gomaxprocs_threads 32`,
		`go_goroutines 128`,
		`forcad_log_dropped_total 0`,
		`forcad_database_connections_in_use 16`,
		`forcad_redis_pool_timeouts_total 0`,
		`forcad_redis_connections 32`,
		`forcad_redis_connections_idle 24`,
		`forcad_flag_submission_phase_duration_seconds_bucket{phase="admission",le="1"} 42`,
		`forcad_websocket_connections 224`,
	} {
		if !keepPoolMetric(line) {
			t.Errorf("diagnostic scrape dropped %q", line)
		}
	}
	for _, line := range []string{
		`# HELP go_gc_duration_seconds A summary of pauses`,
		`# TYPE go_gc_duration_seconds summary`,
		`go_gc_duration_seconds_secret 123`,
		`go_threads_extra 42`,
		`forcad_redis_connections_secret 1`,
		`forcad_redis_connections_idle_extra 1`,
		`go_memstats_other_bytes 123`,
		`unrelated_metric{secret="not-retained"} 1`,
		``,
	} {
		if keepPoolMetric(line) {
			t.Errorf("diagnostic whitelist admitted %q", line)
		}
	}
}

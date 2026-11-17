package load_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestDiagnosticBurstWorkerOverrideDoesNotChangeOrdinaryStages(t *testing.T) {
	for _, raw := range []string{"", "64", "256", "invalid", "0"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("LOAD_DIAGNOSTIC_BURST_WORKERS", raw)
			for _, stageName := range []string{"warmup", "steady-01", "recovery", "step-10000"} {
				limit, err := diagnosticFlagWorkerLimit(stageName)
				if err != nil || limit != flagWorkers {
					t.Fatalf("ordinary stage %q limit=%d err=%v", stageName, limit, err)
				}
			}
		})
	}
}

func TestDiagnosticBurstWorkerOverrideBounds(t *testing.T) {
	for _, setting := range []struct {
		raw  string
		want uint64
	}{
		{
			raw:  "",
			want: 64,
		}, {
			raw:  "1",
			want: 1,
		}, {
			raw:  "64",
			want: 64,
		},
		{
			raw:  "256",
			want: 256,
		}, {
			raw:  "1024",
			want: 1024,
		},
	} {
		t.Run("valid_"+setting.raw, func(t *testing.T) {
			t.Setenv("LOAD_DIAGNOSTIC_BURST_WORKERS", setting.raw)
			limit, err := diagnosticFlagWorkerLimit("flag-burst")
			if err != nil || limit != setting.want {
				t.Fatalf("limit=%d err=%v; want %d", limit, err, setting.want)
			}
		})
	}
	for _, raw := range []string{"0", "-1", "1025", "1.5", "invalid", "999999999999999999999"} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			t.Setenv("LOAD_DIAGNOSTIC_BURST_WORKERS", raw)
			limit, err := diagnosticFlagWorkerLimit("flag-burst")
			if err == nil || limit != 0 {
				t.Fatalf("invalid setting returned limit=%d err=%v", limit, err)
			}
		})
	}
}

func TestAttackReportsAppliedWorkerLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	for _, workers := range []uint64{1, 256} {
		report := attack(context.Background(), attackOptions{
			rps:      100,
			duration: 100 * time.Millisecond,
			workers:  workers,
			target: vegeta.NewStaticTargeter(vegeta.Target{
				Method: http.MethodGet,
				URL:    server.URL,
			}),
			validate: validateRead(false),
		})
		body, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]json.RawMessage
		if err := json.Unmarshal(body, &saved); err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]uint64{
			"generator_worker_limit":    workers,
			"generator_initial_workers": min(workers, 32),
		} {
			var value uint64
			if err := json.Unmarshal(saved[name], &value); err != nil || value != want {
				t.Fatalf("report %s=%s err=%v; want %d", name, saved[name], err, want)
			}
		}
	}
}

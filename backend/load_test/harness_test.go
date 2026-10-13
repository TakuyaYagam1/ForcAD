package load_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestProfiles(t *testing.T) {
	t.Setenv("LOAD_REST_RPS", "")
	t.Setenv("LOAD_WARMUP", "1s")
	t.Setenv("LOAD_RECOVERY", "1s")
	t.Setenv("LOAD_DURATION", "1s")
	for _, name := range []string{"smoke", "load", "soak", "stress", "endpoints", "spike"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("LOAD_PROFILE", name)
			_, stages, err := loadProfile()
			if err != nil {
				t.Fatal(err)
			}
			if stages[0].Name != "warmup" || stages[len(stages)-1].Name != "recovery" {
				t.Fatal("missing warmup or recovery")
			}
			if name == "smoke" || name == "load" || name == "soak" {
				if stages[1].ReadRPS != 500 || stages[1].Overload {
					t.Fatal("steady profile must default to 500 RPS with strict SLO checks")
				}
			}
			if name == "endpoints" {
				seen := map[string]bool{}
				for _, s := range stages {
					if s.ReadRPS == 10000 {
						seen[s.Path] = true
					}
				}
				if len(seen) != len(readPaths) {
					t.Fatal("every endpoint must reach the 10k offered RPS step")
				}
			}
		})
	}
}

func TestProfilesRejectInvalidSettings(t *testing.T) {
	t.Setenv("LOAD_REST_RPS", "")
	for _, raw := range []string{"0", "-1s", "garbage", "24h"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("LOAD_DURATION", raw)
			if _, _, err := loadProfile(); err == nil {
				t.Fatal("invalid duration accepted")
			}
		})
	}
	t.Setenv("LOAD_PROFILE", "unknown")
	if _, _, err := loadProfile(); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestSteadyProfilesRESTOverride(t *testing.T) {
	t.Setenv("LOAD_CHECKPOINT_INTERVAL", "")
	t.Setenv("LOAD_REST_RPS", "10000")
	t.Setenv("LOAD_DURATION", "6m")
	t.Setenv("LOAD_WARMUP", "2s")
	t.Setenv("LOAD_RECOVERY", "3s")
	for _, name := range []string{"smoke", "load", "soak"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("LOAD_PROFILE", name)
			_, stages, err := loadProfile()
			if err != nil {
				t.Fatal(err)
			}
			if len(stages) != 4 || stages[1].Duration != 5*time.Minute || stages[2].Duration != time.Minute {
				t.Fatalf("expected six minutes split into two measurement stages: %+v", stages)
			}
			for _, s := range stages[1 : len(stages)-1] {
				if s.ReadRPS != 10000 || s.FlagRPS != 40 || s.Overload {
					t.Fatalf("10k steady measurement must preserve flags and strict SLO checks: %+v", s)
				}
			}
			if stages[0].ReadRPS != 50 || stages[0].Duration != 2*time.Second || stages[3].ReadRPS != 100 || stages[3].Duration != 3*time.Second {
				t.Fatal("REST override changed warmup or recovery")
			}
		})
	}
}

func TestProfilesRejectInvalidRESTOverride(t *testing.T) {
	t.Setenv("LOAD_PROFILE", "load")
	for _, raw := range []string{"0", "-1", "garbage", "1.5", "100001", "999999999999999999999"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("LOAD_REST_RPS", raw)
			if _, _, err := loadProfile(); err == nil {
				t.Fatal("invalid REST RPS accepted")
			}
		})
	}
	for _, name := range []string{"stress", "endpoints", "spike"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("LOAD_PROFILE", name)
			t.Setenv("LOAD_REST_RPS", "10000")
			if _, _, err := loadProfile(); err == nil {
				t.Fatal("REST RPS override was silently ignored by a step profile")
			}
		})
	}
}

func TestSaturatedGeneratorCannotPassSLO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if waitContext(r.Context(), 20*time.Millisecond) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	report := attack(t.Context(), attackOptions{rps: 1000, duration: 200 * time.Millisecond, workers: 1, target: readTarget(server.URL, "/"), validate: validateRead(false)})
	if report.GeneratorValid || report.SLOPassed {
		t.Fatalf("under-delivery reported as success: %+v", report)
	}
	if report.Failures != 0 {
		t.Fatalf("test handler failed: %+v", report)
	}
}

func TestFarmRejectsFalseVerdicts(t *testing.T) {
	for _, message := range []string{"Flag is invalid", "Flag expired", "service unavailable"} {
		f := newFarm("http://127.0.0.1", &catalog{})
		if err := f.record(1, "flag", message); err == nil {
			t.Fatalf("live foreign flag accepted with verdict %q", message)
		}
	}
	f := newFarm("http://127.0.0.1", &catalog{})
	if err := f.record(1, "flag", "Flag accepted!"); err != nil {
		t.Fatal(err)
	}
	if err := f.record(1, "flag", "Flag already stolen"); err != nil {
		t.Fatal(err)
	}
	if f.accepted != 1 || len(f.expected[1]) != 1 {
		t.Fatal("duplicate inflated accepted count")
	}
}

func TestFarmValidatesResponseOrderAndLength(t *testing.T) {
	for _, size := range []int{batchSize - 1, batchSize} {
		f := newFarm("http://127.0.0.1", &catalog{})
		var target vegeta.Target
		if err := f.target(&target); err != nil {
			t.Fatal(err)
		}
		rows := make([]map[string]string, size)
		for i := range rows {
			rows[i] = map[string]string{"flag": "wrong", "msg": "Flag is invalid"}
		}
		body, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.validate(&vegeta.Result{URL: target.URL, Code: 200, Body: body}); err == nil {
			t.Fatal("incomplete or reordered response accepted")
		}
	}
}

func TestOwnOnlyCatalogDoesNotPanic(t *testing.T) {
	f := newFarm("http://127.0.0.1", &catalog{flags: []storedFlag{{ID: "own", Value: "own-flag", Owner: 1}}})
	request := f.batch(1, 0)
	if request.flags[0].ID != "" || request.flags[70].ID != "own" {
		t.Fatal("own flag used in foreign slots")
	}
}

package load_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartDensityOutOfOrderAndLateStarts(t *testing.T) {
	density := newStartDensity(2 * time.Millisecond)
	for _, elapsed := range []time.Duration{11000, 100, 9900, 1000, 1500, 900, 10001} {
		density.add(elapsed * time.Microsecond)
	}
	oneMS, tenMS := density.peaks()
	if oneMS != 2 || tenMS != 5 {
		t.Fatalf("fixed-window peaks = (%d, %d), want (2, 5)", oneMS, tenMS)
	}
}

func TestStartDensityFixedWindowBoundaries(t *testing.T) {
	density := newStartDensity(20 * time.Millisecond)
	oneMS, tenMS := density.peaks()
	if oneMS != 0 || tenMS != 0 {
		t.Fatal("empty measurement recorded request starts")
	}
	for range 3 {
		density.add(10*time.Millisecond - time.Nanosecond)
	}
	for range 4 {
		density.add(10 * time.Millisecond)
	}
	oneMS, tenMS = density.peaks()
	if oneMS != 4 || tenMS != 4 {
		t.Fatalf("fixed-window peaks = (%d, %d), want (4, 4), not a sliding-window count", oneMS, tenMS)
	}
}

func TestAttackReportsGeneratorDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if waitContext(r.Context(), 5*time.Millisecond) {
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	report := attack(t.Context(), attackOptions{rps: 1000, duration: 50 * time.Millisecond, workers: 1, target: readTarget(server.URL, "/"), validate: validateRead(false)})
	if report.LagMaxMS <= 0 || report.LagP999MS < report.LagP99MS || report.LagP999MS > report.LagMaxMS {
		t.Fatalf("invalid generator lag tail: p99=%f p999=%f max=%f", report.LagP99MS, report.LagP999MS, report.LagMaxMS)
	}
	if report.MaxStarts1MS == 0 || report.MaxStarts10MS < report.MaxStarts1MS || report.MaxStarts10MS > report.Requests {
		t.Fatalf("invalid generator start density: %+v", report)
	}
	if report.GeneratorValid || report.SLOPassed {
		t.Fatal("diagnostics changed the validity of an under-delivered attack")
	}
}

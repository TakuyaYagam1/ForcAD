package load_test

import (
	"testing"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

func TestSpectatorCount(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    int
		invalid bool
	}{
		{"", defaultTeamCount, false}, {"224", 224, false}, {"4096", 4096, false},
		{"0", 0, true}, {"4097", 0, true}, {"invalid", 0, true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("LOAD_SPECTATORS", tc.raw)
			got, err := spectatorCount()
			if got != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("spectatorCount() = %d, %v", got, err)
			}
		})
	}
}

func TestFinalReadTargetCyclesClientBuckets(t *testing.T) {
	t.Setenv("LOAD_CAPACITY", "chrp-final")
	t.Setenv("LOAD_CLIENT_BUCKETS", "0")
	t.Setenv("LOAD_SPECTATORS", "2")
	assertReadTargetCyclesClientBuckets(t)
}

func TestReadTargetCyclesClientBuckets(t *testing.T) {
	t.Setenv("LOAD_CAPACITY", "")
	t.Setenv("LOAD_CLIENT_BUCKETS", "1")
	t.Setenv("LOAD_SPECTATORS", "2")
	assertReadTargetCyclesClientBuckets(t)
}

func assertReadTargetCyclesClientBuckets(t *testing.T) {
	t.Helper()
	targeter := readTarget("http://127.0.0.1:8080", "/api/client/status/")
	for _, expected := range []string{"client-0", "client-1", "client-0"} {
		var target vegeta.Target
		if err := targeter(&target); err != nil {
			t.Fatal(err)
		}
		if target.Header.Get("X-Forcad-Load-Client") != expected {
			t.Fatalf("unexpected client header: %v", target.Header)
		}
	}
}

func TestOrdinaryReadTargetDoesNotSetTestClient(t *testing.T) {
	t.Setenv("LOAD_CAPACITY", "")
	t.Setenv("LOAD_CLIENT_BUCKETS", "0")
	var target vegeta.Target
	if err := readTarget("http://127.0.0.1:8080", "")(&target); err != nil {
		t.Fatal(err)
	}
	if target.Header.Get("X-Forcad-Load-Client") != "" {
		t.Fatal("test client header leaked outside the client bucket fixture")
	}
}

func TestCheckerAuditEnabled(t *testing.T) {
	for _, tc := range []struct {
		name     string
		profile  string
		audit    string
		capacity string
		want     bool
	}{
		{"checker", "checker", "", "", true},
		{"checker explicit off", "checker", "0", "", true},
		{"ordinary", "load", "", "", false},
		{"ordinary explicit off", "load", "0", "", false},
		{"explicit audit", "load", "1", "", true},
		{"soak audit", "soak", "1", "", true},
		{"final capacity", "load", "0", "chrp-final", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LOAD_CHECKER_AUDIT", tc.audit)
			t.Setenv("LOAD_CAPACITY", tc.capacity)
			if got := checkerAuditEnabled(tc.profile); got != tc.want {
				t.Fatalf("checkerAuditEnabled(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}

package load_test

import (
	"strings"
	"testing"
)

func TestAttackGateRejectsInsufficientOverloadGeneration(t *testing.T) {
	report := attackReport{
		Requests: 923,
		Statuses: map[string]int{
			"200": 901,
			"429": 22,
		},
		DeliveryRatio: 923.0 / 1200,
		SkippedSlots:  277,
	}
	if err := checkAttack("flags", report, true); err == nil || !strings.Contains(err.Error(), "generator") {
		t.Fatalf("under-delivered overload passed qualification: %v", err)
	}
	// A successful application SLO cannot make an invalid generator acceptable.
	report.SLOPassed = true
	if err := checkAttack("flags", report, true); err == nil {
		t.Fatal("application SLO bypassed the overload generator gate")
	}
}

func TestAttackGateAcceptsValidControlledRejectionsWithoutRelaxingSteadySLO(t *testing.T) {
	report := attackReport{
		GeneratorValid: true,
		Statuses: map[string]int{
			"200": 900,
			"429": 300,
		},
	}
	if err := checkAttack("flags", report, true); err != nil {
		t.Fatalf("valid controlled overload was rejected: %v", err)
	}
	if err := checkAttack("flags", report, false); err == nil {
		t.Fatal("overload exception weakened the steady SLO")
	}
	report.Failures, report.FirstError = 1, "transport deadline"
	if err := checkAttack("flags", report, true); err == nil || !strings.Contains(err.Error(), "transport deadline") {
		t.Fatalf("overload hid a transport failure: %v", err)
	}
}

package load_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRecoveryRunsAfterFailedMeasurement(t *testing.T) {
	measurementErr := errors.New("flag request timed out")
	recoveryErr := errors.New("recovery still unavailable")
	var observed []string
	err := runStages(t.Context(), []stage{{Name: "baseline"}, {Name: "spike"}, {Name: "recovery"}}, true, func(s stage) error {
		observed = append(observed, s.Name)
		if s.Name == "recovery" {
			return recoveryErr
		}
		return measurementErr
	})
	if !reflect.DeepEqual(observed, []string{"baseline", "recovery"}) {
		t.Fatalf("measurement continued increasing pressure or skipped recovery: %v", observed)
	}
	if !errors.Is(err, measurementErr) || !errors.Is(err, recoveryErr) {
		t.Fatalf("measurement or recovery failure was lost: %v", err)
	}
}

func TestSuccessfulRecoveryPreservesBurstFailure(t *testing.T) {
	burstErr := errors.New("burst response did not match committed flags")
	var observed []string
	err := runStages(t.Context(), []stage{{Name: "steady"}, {Name: "recovery"}}, true, func(s stage) error {
		observed = append(observed, s.Name)
		if s.Name == "flag-burst" {
			return burstErr
		}
		return nil
	})
	if !reflect.DeepEqual(observed, []string{"steady", "flag-burst", "recovery"}) || !errors.Is(err, burstErr) {
		t.Fatalf("failed burst did not retain its failure through recovery: stages=%v error=%v", observed, err)
	}
}

func TestCanceledMeasurementDoesNotStartRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	err := runStages(ctx, []stage{{Name: "steady"}, {Name: "recovery"}}, true, func(stage) error {
		calls++
		cancel()
		return context.Canceled
	})
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation did not stop the scenario: calls=%d error=%v", calls, err)
	}
}

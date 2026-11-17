package prometheus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDependencyCheckPreservesCancellationAndLocation(t *testing.T) {
	check := checkFunc(func(context.Context) error {
		return context.Canceled
	})
	err := check.Check(t.Context())
	if !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "prometheus.checkFunc.Check - ") {
		t.Fatalf("dependency check lost cancellation or attribution: %v", err)
	}
}

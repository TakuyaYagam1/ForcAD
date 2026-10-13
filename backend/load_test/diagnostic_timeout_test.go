package load_test

import (
	"testing"
	"time"
)

func TestFlagLogTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		elapsed time.Duration
		want    time.Duration
	}{
		{name: "before measurement", elapsed: 0, want: 2 * time.Minute},
		{name: "short run", elapsed: 5 * time.Minute, want: 2 * time.Minute},
		{name: "minimum boundary", elapsed: 20 * time.Minute, want: 2 * time.Minute},
		{name: "above minimum", elapsed: 21 * time.Minute, want: 2*time.Minute + 6*time.Second},
		{name: "five hour soak", elapsed: 5 * time.Hour, want: 30 * time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := flagLogTimeout(test.elapsed); got != test.want {
				t.Fatalf("flagLogTimeout(%s) = %s, want %s", test.elapsed, got, test.want)
			}
		})
	}
}

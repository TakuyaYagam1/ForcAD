package load_test

import (
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// Result.Latency ends before Vegeta sends the result on its unbuffered channel.
// Keep this gap separate from hit latency so a slow consumer is observable.
func resultConsumerDelay(result *vegeta.Result, received time.Time) (time.Duration, bool) {
	if result == nil || result.Timestamp.IsZero() || result.Latency < 0 || received.Before(result.Timestamp) {
		return 0, false
	}
	return max(0, received.Sub(result.Timestamp)-result.Latency), true
}

func TestResultConsumerDelay(t *testing.T) {
	started := time.Now()
	result := &vegeta.Result{
		Timestamp: started,
		Latency:   20 * time.Millisecond,
	}
	if delay, valid := resultConsumerDelay(result, started.Add(35*time.Millisecond)); !valid || delay != 15*time.Millisecond {
		t.Fatalf("post-hit delay = %s, valid=%t; want 15ms", delay, valid)
	}
	if delay, valid := resultConsumerDelay(result, started.Add(19*time.Millisecond)); !valid || delay != 0 {
		t.Fatalf("negative gap was not clamped: %s, valid=%t", delay, valid)
	}
	if _, valid := resultConsumerDelay(result, started.Add(-time.Millisecond)); valid {
		t.Fatal("future hit timestamp accepted")
	}
	if _, valid := resultConsumerDelay(nil, started); valid {
		t.Fatal("nil result accepted")
	}
	if _, valid := resultConsumerDelay(&vegeta.Result{}, started); valid {
		t.Fatal("missing timestamp accepted")
	}
	result.Latency = -time.Nanosecond
	if _, valid := resultConsumerDelay(result, started); valid {
		t.Fatal("negative hit latency accepted")
	}
}

package load_test

import (
	"testing"
	"time"
)

func TestContinuousLoadCheckpoint(t *testing.T) {
	t.Setenv("LOAD_PROFILE", "load")
	t.Setenv("LOAD_REST_RPS", "10000")
	t.Setenv("LOAD_DURATION", "15m")
	t.Setenv("LOAD_CHECKPOINT_INTERVAL", "15m")
	_, stages, err := loadProfile()
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 3 || stages[1].Duration != 15*time.Minute || stages[1].ReadRPS != 10000 || stages[1].Overload {
		t.Fatalf("expected one continuous strict measurement: %+v", stages)
	}
	t.Setenv("LOAD_CHECKPOINT_INTERVAL", "0s")
	if _, _, err := loadProfile(); err == nil {
		t.Fatal("invalid checkpoint interval accepted")
	}
}

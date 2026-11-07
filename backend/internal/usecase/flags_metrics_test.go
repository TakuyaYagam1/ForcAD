package usecase

import (
	"context"
	"strings"
	"testing"
	"time"
	"uuid"
)

type flagObserver struct {
	team   uuid.UUID
	counts map[string]int
	phases map[string]int
}

func (o *flagObserver) ResolveTeam(_ context.Context, team uuid.UUID) {
	o.team = team
}
func (o *flagObserver) ObserveFlags(_ uuid.UUID, verdict string, count int) {
	o.counts[verdict] += count
}
func (o *flagObserver) ObserveSubmissionPhase(phase, outcome string, _ time.Duration) {
	o.phases[phase+"/"+outcome]++
}

func TestFlagMetricsCountFlagsInsteadOfBatches(t *testing.T) {
	service, state := newTestFlags()
	observer := &flagObserver{
		counts: make(map[string]int),
		phases: make(map[string]int),
	}
	service.SetObserver(observer)
	flag := strings.Repeat("A", 31) + "="
	if _, err := service.Submit(t.Context(), "0123456789abcdef", []string{flag, flag, "bad"}); err != nil {
		t.Fatal(err)
	}
	if observer.team == uuid.Nil() || observer.counts["accepted"] != 1 || observer.counts["duplicate"] != 1 || observer.counts["invalid"] != 1 {
		t.Fatalf("unexpected metrics: %+v", observer)
	}
	state.rateAllowed = false
	_, _ = service.Submit(t.Context(), "0123456789abcdef", []string{flag, flag})
	if observer.counts["limited"] != 2 {
		t.Fatal("rate-limited flags not counted")
	}
	state.rateAllowed = true
	state.storageErr = context.DeadlineExceeded
	_, _ = service.Submit(t.Context(), "0123456789abcdef", []string{flag})
	if observer.counts["accepted"] != 1 || observer.counts["unavailable"] != 1 {
		t.Fatal("storage failure counted as accepted")
	}
	if observer.phases["resolve/ok"] != 3 || observer.phases["rate/ok"] != 2 || observer.phases["rate/error"] != 1 ||
		observer.phases["admission/ok"] != 2 || observer.phases["storage/ok"] != 1 || observer.phases["storage/deadline"] != 1 ||
		observer.phases["total/ok"] != 1 || observer.phases["total/error"] != 1 || observer.phases["total/deadline"] != 1 {
		t.Fatalf("submission phase observations: %+v", observer.phases)
	}
}

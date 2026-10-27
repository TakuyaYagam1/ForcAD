package usecase

import (
	"context"
	"strings"
	"testing"
	"uuid"
)

type flagObserver struct {
	team   uuid.UUID
	counts map[string]int
}

func (o *flagObserver) ResolveTeam(_ context.Context, team uuid.UUID) { o.team = team }
func (o *flagObserver) ObserveFlags(_ uuid.UUID, verdict string, count int) {
	o.counts[verdict] += count
}

func TestFlagMetricsCountFlagsInsteadOfBatches(t *testing.T) {
	service, state := newTestFlags()
	observer := &flagObserver{counts: make(map[string]int)}
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
}

package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type flagsState struct {
	life        domain.Lifecycle
	lookupCalls int
	cost        int
	scope       string
	rateAllowed bool
	submitted   map[string]bool
	storageErr  error
}

func (s *flagsState) ActiveIDByToken(context.Context, string) (uuid.UUID, error) {
	return uuid.MustParse("00000000-0000-7000-8000-000000000001"), nil
}
func (s *flagsState) IsActiveTeam(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (s *flagsState) Lifecycle(context.Context) (domain.Lifecycle, error)   { return s.life, nil }
func (s *flagsState) Allow(_ context.Context, scope string, cost, _, _ int) (bool, error) {
	s.cost = cost
	s.scope = scope
	return s.rateAllowed, nil
}
func (s *flagsState) SubmitBatch(_ context.Context, submission domain.FlagSubmission) ([]domain.SubmissionResult, error) {
	s.lookupCalls++
	if s.storageErr != nil {
		return nil, s.storageErr
	}
	if submission.ReceivedAt.IsZero() || submission.ReceivedAt.After(time.Now()) {
		return nil, errors.New("missing receipt time")
	}
	results := make([]domain.SubmissionResult, len(submission.Values))
	for i, value := range submission.Values {
		if s.submitted[value] {
			results[i].Err = domain.ErrFlagAlreadyStolen
			continue
		}
		s.submitted[value] = true
		results[i].Attack = domain.AttackResult{AttackerID: submission.TeamID, AttackerDelta: 2.5}
	}
	return results, nil
}
func (s *flagsState) Publish(context.Context, string, any) error { return nil }

func newTestFlags() (*Flags, *flagsState) {
	state := &flagsState{life: domain.Lifecycle{Config: domain.GameConfig{GameRunning: true, RealRound: 1}}, rateAllowed: true, submitted: map[string]bool{}}
	service := NewFlags(state, state, state, state, state, slog.New(slog.DiscardHandler), 200, 400)
	return service, state
}

func TestBatchChargesEveryFlagAndLooksUpOnce(t *testing.T) {
	service, state := newTestFlags()
	flag := strings.Repeat("A", 31) + "="
	values := make([]string, 100)
	for i := range values {
		values[i] = flag
	}
	results, err := service.Submit(context.Background(), "0123456789abcdef", values)
	if err != nil {
		t.Fatal(err)
	}
	if state.lookupCalls != 1 || state.cost != 100 {
		t.Fatalf("batch queries=%d work units=%d", state.lookupCalls, state.cost)
	}
	if state.scope != "team:00000000-0000-7000-8000-000000000001" {
		t.Fatalf("submission limiter scope: %q", state.scope)
	}
	if results[0].Message != "["+flag+"] Flag accepted! Earned 2.5 flag points!" {
		t.Fatal(results[0].Message)
	}
	if results[99].Message != "["+flag+"] Flag already stolen" {
		t.Fatal(results[99].Message)
	}
}

func TestGameBoundaryAndRateLimitStopDatabaseWork(t *testing.T) {
	service, state := newTestFlags()
	state.life.Config.GameRunning = false
	if _, err := service.Submit(context.Background(), "0123456789abcdef", []string{"bad"}); !errors.Is(err, ErrFinished) || !strings.HasPrefix(err.Error(), "usecase.Flags.Submit - ") {
		t.Fatalf("finished game accepted: %v", err)
	}
	state.life.Config.GameRunning = true
	state.rateAllowed = false
	if _, err := service.Submit(context.Background(), "0123456789abcdef", []string{"bad"}); !errors.Is(err, ErrLimited) {
		t.Fatalf("rate limit ignored: %v", err)
	}
	if state.lookupCalls != 0 || len(state.submitted) != 0 {
		t.Fatal("rejected request reached flag storage")
	}
}

func TestMalformedFlagsProducePerFlagVerdicts(t *testing.T) {
	service, state := newTestFlags()
	results, err := service.Submit(context.Background(), "0123456789abcdef", []string{"", "not-a-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || state.lookupCalls != 0 {
		t.Fatal("malformed flags must not query storage")
	}
	if _, err = service.Submit(context.Background(), "0123456789abcdef", []string{strings.Repeat("A", 33)}); !errors.Is(err, ErrInvalidBatch) {
		t.Fatal("oversized value accepted")
	}
}

func TestStorageFailureHasNoFlagVerdictsOrEvents(t *testing.T) {
	service, state := newTestFlags()
	state.storageErr = context.DeadlineExceeded
	results, err := service.Submit(t.Context(), "0123456789abcdef", []string{strings.Repeat("A", 31) + "="})
	if !errors.Is(err, context.DeadlineExceeded) || results != nil || len(service.events) != 0 {
		t.Fatalf("storage error became a flag verdict or event: results=%+v err=%v events=%d", results, err, len(service.events))
	}
}

func TestEmptyBatchStillConsumesAdmissionBudget(t *testing.T) {
	service, state := newTestFlags()
	if _, err := service.Submit(t.Context(), "0123456789abcdef", nil); err != nil {
		t.Fatal(err)
	}
	if state.cost != 1 || state.lookupCalls != 0 {
		t.Fatalf("empty batch cost=%d queries=%d", state.cost, state.lookupCalls)
	}
}

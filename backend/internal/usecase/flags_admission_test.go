package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type admissionReply struct {
	err     error
	verdict error
}

type admissionCall struct {
	submission domain.FlagSubmission
	reply      chan admissionReply
}

type admissionRepository struct {
	entered chan admissionCall
}

func (r *admissionRepository) SubmitBatch(ctx context.Context, submission domain.FlagSubmission) ([]domain.SubmissionResult, error) {
	call := admissionCall{
		submission: submission,
		reply:      make(chan admissionReply, 1),
	}
	select {
	case r.entered <- call:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case reply := <-call.reply:
		if reply.err != nil {
			return nil, reply.err
		}
		verdict := reply.verdict
		if verdict == nil {
			verdict = domain.ErrFlagNotFound
		}
		results := make([]domain.SubmissionResult, len(submission.Values))
		for i := range results {
			results[i].Err = verdict
		}
		return results, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type admissionRepositoryFunc func(context.Context, domain.FlagSubmission) ([]domain.SubmissionResult, error)

func (f admissionRepositoryFunc) SubmitBatch(ctx context.Context, submission domain.FlagSubmission) ([]domain.SubmissionResult, error) {
	return f(ctx, submission)
}

type admissionResult struct {
	responses []FlagResponse
	err       error
}

func startAdmissionSubmission(ctx context.Context, service *Flags, submission domain.FlagSubmission) <-chan admissionResult {
	done := make(chan admissionResult, 1)
	go func() {
		responses, err := service.submitBatch(ctx, submission)
		done <- admissionResult{
			responses: responses,
			err:       err,
		}
	}()
	return done
}

func admissionSubmission(count int) domain.FlagSubmission {
	values := make([]string, count)
	for i := range values {
		values[i] = strings.Repeat("A", 31) + "="
	}
	return domain.FlagSubmission{
		TeamID:     uuid.MustParse("00000000-0000-7000-8000-000000000001"),
		Values:     values,
		ReceivedAt: time.Now(),
		Generation: 42,
	}
}

func assertNoAdmissionCall(t *testing.T, repository *admissionRepository) {
	t.Helper()
	synctest.Wait()
	select {
	case call := <-repository.entered:
		t.Fatalf("unexpected storage call for %d values", len(call.submission.Values))
	default:
	}
}

func assertAdmissionSucceeded(t *testing.T, done <-chan admissionResult, count int) {
	t.Helper()
	result := <-done
	if result.err != nil || len(result.responses) != count {
		t.Fatalf("submission responses=%d err=%v", len(result.responses), result.err)
	}
}

func assertAdmissionPermitsRestored(t *testing.T, service *Flags) {
	t.Helper()
	if !service.admission.TryAcquire(maxBatchSize) {
		t.Fatal("submission permits leaked")
	}
	service.admission.Release(maxBatchSize)
}

func TestFlagAdmissionSerializesFullBatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		service, _ := newTestFlags()
		repository := &admissionRepository{
			entered: make(chan admissionCall, 4),
		}
		service.attacks = repository
		firstDone := startAdmissionSubmission(ctx, service, admissionSubmission(100))
		first := <-repository.entered
		secondDone := startAdmissionSubmission(ctx, service, admissionSubmission(100))
		assertNoAdmissionCall(t, repository)
		first.reply <- admissionReply{}
		assertAdmissionSucceeded(t, firstDone, 100)
		second := <-repository.entered
		second.reply <- admissionReply{}
		assertAdmissionSucceeded(t, secondDone, 100)
		assertAdmissionPermitsRestored(t, service)
	})
}

func TestFlagAdmissionSmallBatchesShareOnlyValidValueBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		service, _ := newTestFlags()
		repository := &admissionRepository{
			entered: make(chan admissionCall, 4),
		}
		service.attacks = repository
		mixed := admissionSubmission(40)
		mixed.Values = append(mixed.Values, make([]string, 60)...)
		firstDone := startAdmissionSubmission(ctx, service, mixed)
		first := <-repository.entered
		if len(first.submission.Values) != 40 || first.submission.ReceivedAt != mixed.ReceivedAt || first.submission.Generation != mixed.Generation || first.submission.TeamID != mixed.TeamID {
			t.Fatal("admission changed filtered batch or original submission identity")
		}
		secondDone := startAdmissionSubmission(ctx, service, admissionSubmission(60))
		second := <-repository.entered
		thirdDone := startAdmissionSubmission(ctx, service, admissionSubmission(1))
		assertNoAdmissionCall(t, repository)
		first.reply <- admissionReply{}
		assertAdmissionSucceeded(t, firstDone, 100)
		third := <-repository.entered
		// The 60-value second batch remains in storage while the third enters.
		third.reply <- admissionReply{}
		second.reply <- admissionReply{}
		assertAdmissionSucceeded(t, secondDone, 60)
		assertAdmissionSucceeded(t, thirdDone, 1)
		assertAdmissionPermitsRestored(t, service)
	})
}

func TestFlagAdmissionCancellationDoesNotReachStorageOrLeakPermits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		service, _ := newTestFlags()
		repository := &admissionRepository{
			entered: make(chan admissionCall, 4),
		}
		service.attacks = repository
		holderDone := startAdmissionSubmission(ctx, service, admissionSubmission(100))
		holder := <-repository.entered
		waitingCtx, cancelWaiting := context.WithCancel(ctx)
		defer cancelWaiting()
		waitingDone := startAdmissionSubmission(waitingCtx, service, admissionSubmission(100))
		assertNoAdmissionCall(t, repository)
		cancelWaiting()
		result := <-waitingDone
		if !errors.Is(result.err, context.Canceled) || result.responses != nil || len(service.events) != 0 {
			t.Fatalf("canceled admission returned responses=%v err=%v events=%d", result.responses, result.err, len(service.events))
		}
		assertNoAdmissionCall(t, repository)
		holder.reply <- admissionReply{}
		assertAdmissionSucceeded(t, holderDone, 100)
		// A new full batch proves that canceled waiting did not retain any weight.
		nextDone := startAdmissionSubmission(ctx, service, admissionSubmission(100))
		next := <-repository.entered
		next.reply <- admissionReply{}
		assertAdmissionSucceeded(t, nextDone, 100)
		assertAdmissionPermitsRestored(t, service)
	})
}

func TestFlagAdmissionCanceledBeforeAcquireLeavesPermitsAvailable(t *testing.T) {
	service, state := newTestFlags()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	responses, err := service.submitBatch(ctx, admissionSubmission(100))
	if !errors.Is(err, context.Canceled) || responses != nil || state.lookupCalls != 0 || len(service.events) != 0 {
		t.Fatalf("pre-canceled admission returned responses=%v err=%v calls=%d", responses, err, state.lookupCalls)
	}
	assertAdmissionPermitsRestored(t, service)
}

func TestFlagAdmissionInvalidOnlyBypassesSaturatedGate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		service, state := newTestFlags()
		repository := &admissionRepository{
			entered: make(chan admissionCall, 4),
		}
		service.attacks = repository
		holderDone := startAdmissionSubmission(ctx, service, admissionSubmission(100))
		holder := <-repository.entered
		responses, err := service.Submit(ctx, "0123456789abcdef", []string{"", "invalid", "lowercase"})
		if err != nil || len(responses) != 3 || state.cost != 3 {
			t.Fatalf("invalid batch responses=%d err=%v limiter_cost=%d", len(responses), err, state.cost)
		}
		for _, response := range responses {
			if response.Verdict != "invalid" {
				t.Fatalf("unexpected invalid-only verdict %q", response.Verdict)
			}
		}
		assertNoAdmissionCall(t, repository)
		holder.reply <- admissionReply{}
		assertAdmissionSucceeded(t, holderDone, 100)
		assertAdmissionPermitsRestored(t, service)
	})
}

func TestFlagAdmissionStorageErrorReleasesPermits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		service, _ := newTestFlags()
		repository := &admissionRepository{
			entered: make(chan admissionCall, 4),
		}
		service.attacks = repository
		done := startAdmissionSubmission(ctx, service, admissionSubmission(100))
		call := <-repository.entered
		call.reply <- admissionReply{
			err: context.DeadlineExceeded,
		}
		result := <-done
		if !errors.Is(result.err, context.DeadlineExceeded) || result.responses != nil || len(service.events) != 0 {
			t.Fatalf("storage error returned responses=%v err=%v events=%d", result.responses, result.err, len(service.events))
		}
		assertAdmissionPermitsRestored(t, service)
	})
}

func TestFlagAdmissionOversizedInternalBatchRejectedBeforeStorage(t *testing.T) {
	service, state := newTestFlags()
	responses, err := service.submitBatch(context.Background(), admissionSubmission(101))
	if !errors.Is(err, ErrInvalidBatch) || responses != nil || state.lookupCalls != 0 {
		t.Fatalf("oversized internal batch returned responses=%v err=%v calls=%d", responses, err, state.lookupCalls)
	}
	assertAdmissionPermitsRestored(t, service)
}

func TestFlagAdmissionRepositoryPanicReleasesPermits(t *testing.T) {
	service, _ := newTestFlags()
	service.attacks = admissionRepositoryFunc(func(context.Context, domain.FlagSubmission) ([]domain.SubmissionResult, error) {
		panic("repository test panic")
	})
	func() {
		defer func() {
			if value := recover(); value != "repository test panic" {
				t.Errorf("unexpected recovered panic: %v", value)
			}
		}()
		_, _ = service.submitBatch(t.Context(), admissionSubmission(100))
		t.Error("expected repository panic")
	}()
	assertAdmissionPermitsRestored(t, service)
}

type admissionBlockedVerdictError struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *admissionBlockedVerdictError) Error() string {
	e.once.Do(func() {
		close(e.started)
	})
	<-e.release
	return "unexpected test verdict"
}

func TestFlagAdmissionReleasesBeforeVerdictFormatting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		service, _ := newTestFlags()
		repository := &admissionRepository{
			entered: make(chan admissionCall, 4),
		}
		service.attacks = repository
		blocked := &admissionBlockedVerdictError{
			started: make(chan struct{}),
			release: make(chan struct{}),
		}
		defer close(blocked.release)
		startAdmissionSubmission(ctx, service, admissionSubmission(100))
		first := <-repository.entered
		first.reply <- admissionReply{
			verdict: blocked,
		}
		<-blocked.started
		// The first request is still formatting its verdict, not using storage.
		secondDone := startAdmissionSubmission(ctx, service, admissionSubmission(100))
		second := <-repository.entered
		second.reply <- admissionReply{}
		assertAdmissionSucceeded(t, secondDone, 100)
		assertAdmissionPermitsRestored(t, service)
	})
}

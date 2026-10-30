package checker

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

// Repository recovery already proves the exact SQL owner. These tests verify
// that a definitive read-back does not resurrect the worker's canceled parent
// or original lease window, and does not reuse an expired RPC context.
type recoveredRepository struct {
	*fakeRepository

	afterClaim func(*domain.CheckerJob)
	deadline   func(context.Context) (bool, error)
	renew      func(context.Context) (bool, error)
}

func (r *recoveredRepository) Claim(ctx context.Context, id uuid.UUID, owner string, at, deadline time.Time) (domain.CheckerJob, error) {
	job, err := r.fakeRepository.Claim(ctx, id, owner, at, deadline)
	if err == nil && r.afterClaim != nil {
		r.afterClaim(&job)
	}
	return job, err
}

func (r *recoveredRepository) SetDeadline(ctx context.Context, id uuid.UUID, owner string, at, deadline time.Time) (bool, error) {
	if r.deadline != nil {
		return r.deadline(ctx)
	}
	return r.fakeRepository.SetDeadline(ctx, id, owner, at, deadline)
}

func (r *recoveredRepository) Renew(ctx context.Context, id uuid.UUID, owner string, at, deadline time.Time) (bool, error) {
	if r.renew != nil {
		return r.renew(ctx)
	}
	return r.fakeRepository.Renew(ctx, id, owner, at, deadline)
}

func TestConfirmedClaimCannotReviveCanceledOrExpiredOwner(t *testing.T) {
	for _, scenario := range []string{"canceled", "expired", "shortened"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				repo := &recoveredRepository{fakeRepository: newFakeRepository(nil, domain.JobActionCheckOnly)}
				repo.afterClaim = func(job *domain.CheckerJob) {
					switch scenario {
					case "canceled":
						cancel()
					case "expired":
						time.Sleep(time.Second)
					case "shortened":
						expired := time.Now()
						job.ClaimDeadlineAt = &expired
					}
				}
				worker := New(repo, nil, nil, nil, Options{LeaseDuration: time.Second}, nil)
				_, accepted, err := worker.claimJob(ctx, testJobID)
				want := ErrLeaseLost
				if scenario == "canceled" {
					want = context.Canceled
				}
				if accepted || !errors.Is(err, want) {
					t.Fatalf("confirmed claim accepted=%v err=%v", accepted, err)
				}
			})
		})
	}
}

func TestConfirmedLeaseUsesLiveParentAndOriginalWindow(t *testing.T) {
	for _, operation := range []string{"deadline", "renew"} {
		for _, scenario := range []string{"rpc_expired", "parent_canceled", "lease_expired"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithCancelCause(t.Context())
					defer cancel(context.Canceled)
					repo := &recoveredRepository{fakeRepository: newFakeRepository(nil, domain.JobActionCheckOnly)}
					confirmed := func(rpc context.Context) {
						switch scenario {
						case "rpc_expired":
							<-rpc.Done()
						case "parent_canceled":
							cancel(context.Canceled)
						case "lease_expired":
							time.Sleep(31 * time.Second)
						}
					}
					renewCalls := 0
					repo.renew = func(rpc context.Context) (bool, error) {
						renewCalls++
						if err := rpc.Err(); err != nil {
							t.Fatalf("renewal reused expired RPC context: %v", err)
						}
						if operation == "renew" {
							confirmed(rpc)
						}
						return true, nil
					}
					if operation == "deadline" {
						repo.deadline = func(rpc context.Context) (bool, error) {
							confirmed(rpc)
							return true, nil // Exact-owner read-back succeeded.
						}
					}
					worker := New(repo, nil, nil, nil, Options{LeaseDuration: 30 * time.Second, RenewInterval: time.Second}, nil)
					guard := &leaseGuard{worker: worker, jobID: testJobID, ownerToken: "owner", ctx: ctx, cancelFn: cancel,
						workflowDeadline: time.Now().Add(time.Minute), recoveryDeadline: time.Now().Add(2 * time.Minute)}
					err := guard.renewNow()
					switch scenario {
					case "rpc_expired":
						if err != nil || renewCalls != 1 || guard.failure() != nil {
							t.Fatalf("definitive recovery failed: %v calls=%d", err, renewCalls)
						}
					case "parent_canceled":
						if !errors.Is(err, context.Canceled) {
							t.Fatalf("canceled owner revived: %v", err)
						}
					case "lease_expired":
						if !errors.Is(err, ErrLeaseLost) {
							t.Fatalf("expired original window revived: %v", err)
						}
					}
					if scenario != "rpc_expired" && operation == "deadline" && renewCalls != 0 {
						t.Fatal("invalid confirmed deadline continued into lease renewal")
					}
				})
			})
		}
	}
}

package checker

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type jobClaim struct {
	job        domain.CheckerJob
	ownerToken string
	claimedAt  time.Time
}

func (w *Worker) claimJob(ctx context.Context, jobID uuid.UUID) (jobClaim, bool, error) {
	const location = "checker.Worker.claimJob"
	ownerToken, err := newOwnerToken()
	if err != nil {
		return jobClaim{}, false, fmt.Errorf("%s - create checker claim token: %w", location, err)
	}
	now := time.Now()
	job, err := w.repo.Claim(ctx, jobID, ownerToken, now, now.Add(w.options.LeaseDuration))
	if err != nil {
		if settleErr := w.settleClaimFailure(ctx, jobID, err); settleErr != nil {
			return jobClaim{}, false, fmt.Errorf("%s - claim checker job: %w", location, settleErr)
		}
		return jobClaim{}, false, nil
	}
	if job.ID != jobID || job.ClaimToken == nil || *job.ClaimToken != ownerToken || job.FinishedAt != nil {
		return jobClaim{}, false, fmt.Errorf("%s - verify checker job claim: %w", location, ErrLeaseLost)
	}
	// A commit may have been confirmed using an independent read after the
	// request expired. Confirmation never revives a canceled or expired owner.
	if err := ctx.Err(); err != nil {
		return jobClaim{}, false, fmt.Errorf("%s - accept confirmed claim: %w", location, context.Cause(ctx))
	}
	claimDeadline := now.Add(w.options.LeaseDuration)
	if job.ClaimDeadlineAt != nil && job.ClaimDeadlineAt.Before(claimDeadline) {
		claimDeadline = *job.ClaimDeadlineAt
	}
	if !time.Now().Before(claimDeadline) {
		return jobClaim{}, false, fmt.Errorf("%s - confirmed claim expired: %w", location, ErrLeaseLost)
	}
	return jobClaim{job: job, ownerToken: ownerToken, claimedAt: now}, true, nil
}

func (w *Worker) finish(ctx context.Context, guard *leaseGuard, job domain.CheckerJob, result domain.CheckerResult, executeErr error) error {
	const location = "checker.Worker.finish"
	if leaseErr := guard.failure(); leaseErr != nil {
		guard.cancelFn(leaseErr)
		<-guard.done
		return fmt.Errorf("%s - maintain checker job lease: %w", location, leaseErr)
	}
	if err := ctx.Err(); err != nil {
		guard.cancelFn(err)
		<-guard.done
		return fmt.Errorf("%s - handle checker job cancellation: %w", location, err)
	}
	workflowTimedOut := errors.Is(executeErr, ErrWorkflowDeadline) || !time.Now().Before(guard.workflowDeadline)
	if executeErr != nil && (!workflowTimedOut || errors.Is(executeErr, ErrWorkerUnreaped)) {
		guard.cancelFn(executeErr)
		<-guard.done
		return fmt.Errorf("%s - execute checker workflow: %w", location, executeErr)
	}
	if workflowTimedOut {
		result = domain.CheckerResult{
			Round: guard.round, Status: workflowTimeoutStatus,
			PublicMessage:  "Checker workflow timed out",
			PrivateMessage: "Checker workflow exceeded CHECKER_WORKFLOW_TIMEOUT",
		}
	}
	result.Logs = guard.completedCheckerLogs()

	var completed bool
	var completeErr error
	completeCtx, cancelComplete := guard.recoveryContext(w.options.RecoveryGrace)
	//nolint:contextcheck // guard.ctx is the request context's lease-aware child.
	completed, completeErr = w.repo.Complete(completeCtx, guard.jobID, guard.ownerToken, time.Now(), result)
	cancelComplete()
	guard.cancelFn(context.Canceled)
	<-guard.done
	if completed {
		payload, _ := decodePayload(job.Payload)
		if w.observer != nil {
			w.observer.ObserveCompleted(payload.Task.Name, payload.Team.Name, result, workflowTimedOut)
		}
		w.logger.Info("checker workflow completed", "job_id", guard.jobID, "checker_service", payload.Task.Name, "team", payload.Team.Name, "round", result.Round, "status", result.Status)
		return nil
	}
	closed, stateErr := w.confirmClosed(ctx, guard.jobID)
	if closed {
		return nil
	}
	if completeErr != nil || stateErr != nil {
		return fmt.Errorf("%s - settle checker job completion: %w", location, errors.Join(completeErr, stateErr, ErrLeaseLost))
	}
	return fmt.Errorf("%s - verify checker job completion: %w", location, ErrLeaseLost)
}

func (w *Worker) settleClaimFailure(ctx context.Context, jobID uuid.UUID, claimErr error) error {
	const location = "checker.Worker.settleClaimFailure"
	if errors.Is(claimErr, domain.ErrCheckerJobClosed) {
		return nil
	}
	if !errors.Is(claimErr, domain.ErrJobClaimed) && !errors.Is(claimErr, domain.ErrJobNotReady) {
		return fmt.Errorf("%s - claim checker job: %w", location, claimErr)
	}
	if closed, stateErr := w.confirmClosed(ctx, jobID); closed {
		return nil
	} else if stateErr != nil {
		return fmt.Errorf("%s - confirm checker job state: %w", location, errors.Join(claimErr, stateErr))
	}
	return fmt.Errorf("%s - checker job is not claimable: %w", location, claimErr)
}

func (w *Worker) confirmClosed(ctx context.Context, jobID uuid.UUID) (bool, error) {
	const location = "checker.Worker.confirmClosed"
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stateCheckTimeout)
	defer cancel()
	state, err := w.repo.State(checkCtx, jobID)
	if errors.Is(err, domain.ErrCheckerJobClosed) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s - read checker job state: %w", location, err)
	}
	if state.FinishedAt != nil {
		return true, nil
	}
	if state.DeadlineAt != nil && !state.DeadlineAt.After(time.Now()) {
		expired, expireErr := w.repo.Expire(checkCtx, jobID, time.Now())
		if expireErr != nil {
			return false, fmt.Errorf("%s - expire checker job: %w", location, expireErr)
		}
		if expired {
			return true, nil
		}
		state, err = w.repo.State(checkCtx, jobID)
		if errors.Is(err, domain.ErrCheckerJobClosed) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("%s - reread checker job state: %w", location, err)
		}
		return state.FinishedAt != nil, nil
	}
	return false, nil
}

func newOwnerToken() (string, error) {
	const location = "checker.newOwnerToken"
	var token [32]byte
	if _, err := cryptorand.Read(token[:]); err != nil {
		return "", fmt.Errorf("%s - generate claim token: %w", location, err)
	}
	return hex.EncodeToString(token[:]), nil
}

package checker

import (
	"context"
	"fmt"
	"sync"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type leaseGuard struct {
	worker           *Worker
	jobID            uuid.UUID
	ownerToken       string
	round            int32
	ctx              context.Context
	cancelFn         context.CancelCauseFunc
	done             chan struct{}
	renewMu          sync.Mutex
	mu               sync.Mutex
	err              error
	actionTimeout    time.Duration
	workflowDeadline time.Time
	recoveryDeadline time.Time
	checkerLogs      []domain.CheckerLog
}

func (g *leaseGuard) appendCheckerLog(log domain.CheckerLog) {
	g.mu.Lock()
	g.checkerLogs = append(g.checkerLogs, log)
	g.mu.Unlock()
}

func (g *leaseGuard) completedCheckerLogs() []domain.CheckerLog {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]domain.CheckerLog(nil), g.checkerLogs...)
}

func (g *leaseGuard) run() {
	defer close(g.done)
	ticker := time.NewTicker(g.worker.options.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			if err := g.renewNow(); err != nil {
				return
			}
		}
	}
}

func (g *leaseGuard) renewNow() error {
	const location = "checker.leaseGuard.renewNow"
	// Serialize lease writes without holding the state/log mutex across I/O.
	// Every write has a deadline, including time waiting for a pooled connection.
	g.renewMu.Lock()
	defer g.renewMu.Unlock()
	g.mu.Lock()
	previousErr, actionTimeout := g.err, g.actionTimeout
	g.mu.Unlock()
	if previousErr != nil {
		return previousErr
	}
	if err := g.ctx.Err(); err != nil {
		return fmt.Errorf("%s - renew checker job lease: %w", location, context.Cause(g.ctx))
	}
	now := time.Now()
	if !now.Before(g.recoveryDeadline) {
		err := fmt.Errorf("%s - workflow recovery grace expired: %w", location, ErrWorkflowRecoveryExpired)
		g.abort(err)
		return err
	}
	deadline := now.Add(actionTimeout + g.worker.options.RecoveryGrace)
	if deadline.After(g.recoveryDeadline) {
		deadline = g.recoveryDeadline
	}
	renewCtx, stopRenew := context.WithTimeout(g.ctx, min(stateCheckTimeout, g.worker.options.RenewInterval))
	deadlineSet, err := g.worker.repo.SetDeadline(renewCtx, g.jobID, g.ownerToken, now, deadline)
	if err != nil {
		defer stopRenew()
		return g.renewFailure(renewCtx, "set checker job deadline", err)
	}
	stopRenew()
	if !deadlineSet {
		return g.renewFailure(g.ctx, "set checker job deadline", ErrLeaseLost)
	}
	if err := g.confirmLeaseWindow(deadline); err != nil {
		return err
	}
	leaseDeadline := now.Add(g.worker.options.LeaseDuration)
	if leaseDeadline.After(g.recoveryDeadline) {
		leaseDeadline = g.recoveryDeadline
	}
	// SetDeadline may have reconciled its commit after its RPC context ended.
	// Renewal has a fresh bounded RPC context, still fenced by the same parent
	// and the original requested deadlines; no checker action is replayed.
	renewCtx, stopRenew = context.WithTimeout(g.ctx, min(stateCheckTimeout, g.worker.options.RenewInterval))
	defer stopRenew()
	owned, err := g.worker.repo.Renew(renewCtx, g.jobID, g.ownerToken, time.Now(), leaseDeadline)
	if err != nil {
		return g.renewFailure(renewCtx, "renew checker job lease", err)
	}
	if !owned {
		return g.renewFailure(renewCtx, "renew checker job lease", ErrLeaseLost)
	}
	return g.confirmLeaseWindow(minTime(deadline, leaseDeadline))
}

func (g *leaseGuard) confirmLeaseWindow(deadline time.Time) error {
	if err := g.ctx.Err(); err != nil {
		return g.renewFailure(g.ctx, "accept confirmed lease", context.Cause(g.ctx))
	}
	if !time.Now().Before(deadline) {
		return g.renewFailure(g.ctx, "confirmed lease window expired", ErrLeaseLost)
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (g *leaseGuard) renewFailure(ctx context.Context, operation string, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		err = cause
	}
	err = fmt.Errorf("checker.leaseGuard.renewNow - %s: %w", operation, err)
	g.abort(err)
	return err
}

func (g *leaseGuard) checkWorkflowWindow() error {
	const location = "checker.leaseGuard.checkWorkflowWindow"
	g.mu.Lock()
	defer g.mu.Unlock()
	if !time.Now().Before(g.workflowDeadline) {
		return fmt.Errorf("%s - workflow deadline exceeded: %w", location, ErrWorkflowDeadline)
	}
	if !time.Now().Before(g.recoveryDeadline) {
		return fmt.Errorf("%s - workflow recovery grace expired: %w", location, ErrWorkflowRecoveryExpired)
	}
	return nil
}

func (g *leaseGuard) abort(err error) {
	g.mu.Lock()
	if g.err == nil {
		g.err = err
	}
	g.mu.Unlock()
	g.cancelFn(err)
}

func (g *leaseGuard) setActionTimeout(timeout time.Duration) {
	g.mu.Lock()
	g.actionTimeout = timeout
	g.mu.Unlock()
}

func (g *leaseGuard) recoveryContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(g.ctx, timeout)
}

func (g *leaseGuard) failure() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}

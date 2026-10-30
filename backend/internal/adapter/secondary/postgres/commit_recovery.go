package postgres

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/jackc/pgx/v5"
)

const commitRecoveryTimeout = time.Second

// confirmJobCommit only observes committed state on a new connection. It never
// retries a claim or PUT. The worker still checks its parent cancellation and
// confirmed lease before continuing after this bounded RPC recovery.
func (r *Repository) confirmJobCommit(ctx context.Context, commitErr error, id uuid.UUID, owner string, requested time.Time, operation string) bool {
	if errors.Is(commitErr, pgx.ErrTxCommitRollback) || !requested.After(time.Now()) {
		return false
	}
	deadline := minTime(time.Now().Add(commitRecoveryTimeout), requested)
	recovery, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	state, err := r.queries.ConfirmCheckerJobOwnership(recovery, sqlc.ConfirmCheckerJobOwnershipParams{JobID: id, OwnerToken: &owner})
	return err == nil && committedJobUpdate(state, requested, operation)
}

func committedJobUpdate(state sqlc.ConfirmCheckerJobOwnershipRow, requested time.Time, operation string) bool {
	if !state.DeadlineAt.Valid || !state.ClaimDeadlineAt.Valid {
		return false
	}
	requested = requested.Truncate(time.Microsecond)
	switch operation {
	case "claim", "renew":
		return !state.ClaimDeadlineAt.Time.Before(minTime(requested, state.DeadlineAt.Time))
	case "deadline":
		if state.ExpiresAt.Valid {
			requested = minTime(requested, state.ExpiresAt.Time)
		}
		return state.DeadlineAt.Time.Equal(requested) && !state.ClaimDeadlineAt.Time.After(requested)
	default:
		return false
	}
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

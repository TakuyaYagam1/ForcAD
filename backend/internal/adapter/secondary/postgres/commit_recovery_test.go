package postgres

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCommittedJobUpdateRequiresRequestedState(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	state := sqlc.ConfirmCheckerJobOwnershipRow{DeadlineAt: timestamp(at.Add(time.Minute)), ClaimDeadlineAt: timestamp(at.Add(20 * time.Second)), ExpiresAt: timestamp(at.Add(2 * time.Minute))}
	if committedJobUpdate(state, at.Add(30*time.Second), "renew") {
		t.Fatal("accepted old same-owner lease as committed renewal")
	}
	if !committedJobUpdate(state, at.Add(20*time.Second+123*time.Nanosecond), "renew") {
		t.Fatal("database timestamp precision broke confirmed renewal")
	}
	if committedJobUpdate(state, at.Add(50*time.Second), "deadline") {
		t.Fatal("accepted old deadline")
	}
	if !committedJobUpdate(state, at.Add(time.Minute), "deadline") {
		t.Fatal("rejected confirmed deadline")
	}
	state.ClaimDeadlineAt = timestamp(at.Add(3 * time.Minute))
	if committedJobUpdate(state, at.Add(time.Minute), "deadline") {
		t.Fatal("accepted lease beyond confirmed deadline")
	}
	state.DeadlineAt.Valid = false
	if committedJobUpdate(state, at.Add(time.Minute), "claim") {
		t.Fatal("accepted invalid state")
	}
}

type recoveryReader struct {
	t     *testing.T
	state sqlc.ConfirmCheckerJobOwnershipRow
	err   error
	reads int
}

func (r *recoveryReader) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("recovery must not write")
}
func (r *recoveryReader) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected recovery query")
}
func (r *recoveryReader) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	r.reads++
	if ctx.Err() != nil {
		r.t.Fatal("recovery reused canceled RPC context")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > commitRecoveryTimeout {
		r.t.Fatal("recovery lacks bounded deadline")
	}
	return r
}
func (r *recoveryReader) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*pgtype.Timestamptz) = r.state.DeadlineAt
	*dest[1].(*pgtype.Timestamptz) = r.state.ClaimDeadlineAt
	*dest[2].(*pgtype.Timestamptz) = r.state.ExpiresAt
	return nil
}

func TestCommitRecoveryUsesBoundedReadOnlyFreshContext(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	reader := &recoveryReader{t: t, state: sqlc.ConfirmCheckerJobOwnershipRow{DeadlineAt: timestamp(at.Add(time.Minute)), ClaimDeadlineAt: timestamp(at.Add(30 * time.Second))}}
	repo := &Repository{queries: sqlc.New(reader)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !repo.confirmJobCommit(ctx, errors.New("connection lost during commit"), uuid.NewV7(), "same-owner", at.Add(30*time.Second), "renew") {
		t.Fatal("did not confirm committed renewal with fresh read")
	}
	reader.err = pgx.ErrNoRows
	if repo.confirmJobCommit(ctx, errors.New("ambiguous commit"), uuid.NewV7(), "same-owner", at.Add(30*time.Second), "renew") {
		t.Fatal("accepted unconfirmed ownership")
	}
	before := reader.reads
	if repo.confirmJobCommit(ctx, pgx.ErrTxCommitRollback, uuid.NewV7(), "same-owner", at.Add(30*time.Second), "renew") || reader.reads != before {
		t.Fatal("reconciled a definitive rollback")
	}
}

package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) SubmitBatch(ctx context.Context, submission domain.FlagSubmission) ([]domain.SubmissionResult, error) {
	const location = "postgres.Repository.SubmitBatch"
	if len(submission.Values) == 0 {
		return []domain.SubmissionResult{}, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("%s - begin submission transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	state, err := lockSubmissionState(ctx, queries, submission)
	if err != nil {
		return nil, fmt.Errorf("%s - load submission state: %w", location, err)
	}
	// Game/session locks fence resets. Immutable flag rows need no extra lock:
	// FOR SHARE would generate WAL even for a batch containing only duplicates.
	flags, err := queries.ReadSubmissionFlags(ctx, sqlc.ReadSubmissionFlagsParams{
		AttackerID: submission.TeamID, FlagValues: submission.Values,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - read submitted flags: %w", location, err)
	}
	batch := newAttackBatch(submission, state, flags)
	if err := batch.lockTasks(ctx, queries); err != nil {
		return nil, fmt.Errorf("%s - lock submission scores: %w", location, err)
	}
	results, err := batch.apply(ctx, queries)
	if err != nil {
		return nil, fmt.Errorf("%s - apply submission: %w", location, err)
	}
	if !slices.ContainsFunc(results, func(result domain.SubmissionResult) bool { return result.Err == nil }) {
		// No points or stolen flags changed. Rollback releases the read locks
		// without making an unsuccessful submission wait for a durable commit.
		return results, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s - commit submission: %w", location, err)
	}
	return results, nil
}

type submissionState struct {
	game       sqlc.LockGameForAttackRow
	round      int32
	generation int64
}

func lockSubmissionState(ctx context.Context, queries *sqlc.Queries, submission domain.FlagSubmission) (submissionState, error) {
	const location = "postgres.lockSubmissionState"
	game, err := queries.LockGameForAttack(ctx)
	if err != nil {
		return submissionState{}, fmt.Errorf("%s - lock game: %w", location, err)
	}
	session, err := queries.LockSessionForAttack(ctx)
	if err != nil {
		return submissionState{}, fmt.Errorf("%s - lock session: %w", location, err)
	}
	phaseStart := session.PracticeStart
	if session.FinalStart.Valid {
		phaseStart = session.FinalStart
	}
	if !game.GameRunning || session.ResetPending || session.PausedAt.Valid ||
		session.Generation != submission.Generation || submission.ReceivedAt.IsZero() ||
		(phaseStart.Valid && submission.ReceivedAt.Before(phaseStart.Time)) {
		return submissionState{}, fmt.Errorf("%s - validate game availability: %w", location, domain.ErrGameUnavailable)
	}
	if game.FlagLifetime <= 0 {
		return submissionState{}, fmt.Errorf("%s - validate flag lifetime: %w", location, domain.ErrInvalidGameConfig)
	}
	// Stamp the decoded batch before waiting for a connection. Recorded round
	// starts keep pool and row-lock waits from expiring a received flag.
	round, err := queries.SubmissionRound(ctx, sqlc.SubmissionRoundParams{
		ReceivedAt: float64(submission.ReceivedAt.UnixNano()) / float64(time.Second), CurrentRound: game.RealRound,
	})
	if err != nil {
		return submissionState{}, fmt.Errorf("%s - resolve round at receipt: %w", location, err)
	}
	if round < 1 {
		return submissionState{}, fmt.Errorf("%s - submission predates the first round: %w", location, domain.ErrGameUnavailable)
	}
	return submissionState{game: game, round: min(round, game.RealRound), generation: session.Generation}, nil
}

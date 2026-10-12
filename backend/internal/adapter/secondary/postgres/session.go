package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Get(ctx context.Context) (domain.Session, error) {
	const location = "postgres.Repository.Get"

	row, err := r.queries.GetSession(ctx)
	if err != nil {
		return domain.Session{}, fmt.Errorf("%s - load game session: %w", location, err)
	}
	return domainSession(row), nil
}

func (r *Repository) SetPaused(
	ctx context.Context,
	paused bool,
	at time.Time,
) (domain.Session, error) {
	const location = "postgres.Repository.SetPaused"

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Session{}, fmt.Errorf("%s - begin session update transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if _, err := queries.LockGameForSession(ctx); err != nil {
		return domain.Session{}, fmt.Errorf("%s - lock game for session update: %w", location, err)
	}
	if _, err := queries.LockSessionForUpdate(ctx); err != nil {
		return domain.Session{}, fmt.Errorf("%s - lock session for update: %w", location, err)
	}
	if paused {
		if _, err := queries.PauseSession(ctx, timestamp(at)); err != nil {
			return domain.Session{}, fmt.Errorf("%s - pause session: %w", location, err)
		}
	} else {
		if _, err := queries.ResumeSession(ctx, timestamp(at)); err != nil {
			return domain.Session{}, fmt.Errorf("%s - resume session: %w", location, err)
		}
	}
	row, err := queries.GetSession(ctx)
	if err != nil {
		return domain.Session{}, fmt.Errorf("%s - reload updated session: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Session{}, fmt.Errorf("%s - commit session update transaction: %w", location, err)
	}
	return domainSession(row), nil
}

func (r *Repository) SaveSnapshot(ctx context.Context, snapshot domain.RoundSnapshot) error {
	const location = "postgres.Repository.SaveSnapshot"

	if snapshot.Round < 1 || snapshot.RoundStart < 0 {
		return fmt.Errorf("%s - validate completed round snapshot", location)
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("%s - encode completed round snapshot: %w", location, err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%s - begin snapshot transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if _, err := queries.LockGameForSession(ctx); err != nil {
		return fmt.Errorf("%s - lock game for snapshot update: %w", location, err)
	}
	if _, err := queries.LockSessionForUpdate(ctx); err != nil {
		return fmt.Errorf("%s - lock session for snapshot update: %w", location, err)
	}
	if _, err := queries.InsertCompletedRoundSnapshot(ctx, sqlc.InsertCompletedRoundSnapshotParams{
		Round: snapshot.Round, RoundStart: float64(snapshot.RoundStart), Snapshot: body,
	}); err != nil {
		return fmt.Errorf("%s - insert completed round snapshot: %w", location, err)
	}
	rows, err := queries.SaveGameSnapshot(ctx, sqlc.SaveGameSnapshotParams{
		SnapshotRound: &snapshot.Round, Snapshot: body,
	})
	if err != nil {
		return fmt.Errorf("%s - update current game snapshot: %w", location, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s - update current game snapshot: %w", location, domain.ErrGameUnavailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s - commit snapshot transaction: %w", location, err)
	}
	return nil
}

func domainSession(row sqlc.Gamesession) domain.Session {
	return domain.Session{
		PracticeStart:         nullableTime(row.PracticeStart),
		FinalStart:            nullableTime(row.FinalStart),
		ResetPending:          row.ResetPending,
		Generation:            row.Generation,
		PausedAt:              nullableTime(row.PausedAt),
		PausedSeconds:         row.PausedSeconds,
		SchedulePausedSeconds: row.SchedulePausedSeconds,
		RoundStartedAt:        nullableTime(row.RoundStartedAt),
		SnapshotRound:         row.SnapshotRound,
		Snapshot:              rawJSON(row.Snapshot),
	}
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Create(ctx context.Context, flag domain.Flag, owner *domain.CheckerJobOwner) (domain.Flag, error) {
	const location = "postgres.Repository.Create"

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - begin flag creation transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)

	queries := r.queries.WithTx(tx)
	config, err := queries.GetFlagInsertConfig(ctx)
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - load flag insertion configuration: %w", location, err)
	}
	if config.FlagLifetime <= 0 || config.RoundTime <= 0 {
		return domain.Flag{}, fmt.Errorf("%s - validate flag insertion configuration: %w", location, domain.ErrInvalidGameConfig)
	}
	session, err := queries.LockSessionForFlag(ctx)
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - lock session for flag insertion: %w", location, err)
	}
	if session.ResetPending {
		return domain.Flag{}, fmt.Errorf("%s - reject flag insertion during game reset: %w", location, domain.ErrCheckerJobClosed)
	}
	if owner != nil {
		if owner.JobID == uuid.Nil() || owner.ClaimToken == "" {
			return domain.Flag{}, fmt.Errorf("%s - validate checker job owner: %w", location, domain.ErrCheckerJobClosed)
		}
		_, err = queries.LockOwnedCheckerJob(ctx, sqlc.LockOwnedCheckerJobParams{
			JobID:      owner.JobID,
			OwnerToken: &owner.ClaimToken,
			ClaimedAt:  timestampNow(),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Flag{}, fmt.Errorf("%s - verify checker job ownership: %w", location, domain.ErrCheckerJobClosed)
		}
		if err != nil {
			return domain.Flag{}, fmt.Errorf("%s - verify checker job ownership: %w", location, err)
		}
	}

	row, err := queries.InsertFlag(ctx, sqlc.InsertFlagParams{
		Flag:            flag.Value,
		TeamID:          flag.TeamID,
		TaskID:          flag.TaskID,
		Round:           flag.Round,
		PublicFlagData:  flag.PublicData,
		PrivateFlagData: flag.PrivateData,
		VulnNumber:      flag.VulnerabilityID,
	})
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - insert flag: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Flag{}, fmt.Errorf("%s - commit flag creation transaction: %w", location, err)
	}
	return domainFlag(row), nil
}

func (r *Repository) FindLiveByValue(ctx context.Context, value string, currentRound int32) (domain.Flag, error) {
	const location = "postgres.Repository.FindLiveByValue"

	row, err := r.queries.GetLiveFlagByValue(ctx, sqlc.GetLiveFlagByValueParams{
		Flag:         value,
		CurrentRound: int64(currentRound),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Flag{}, fmt.Errorf("%s - find live flag by value: %w", location, domain.ErrFlagNotFound)
	}
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - find live flag by value: %w", location, err)
	}
	return domainFlag(row), nil
}

func (r *Repository) FindLiveByID(ctx context.Context, id uuid.UUID, currentRound int32) (domain.Flag, error) {
	const location = "postgres.Repository.FindLiveByID"

	row, err := r.queries.GetLiveFlagByID(ctx, sqlc.GetLiveFlagByIDParams{
		ID:           id,
		CurrentRound: int64(currentRound),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Flag{}, fmt.Errorf("%s - find live flag by ID: %w", location, domain.ErrFlagNotFound)
	}
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - find live flag by ID: %w", location, err)
	}
	return domainFlag(row), nil
}

func (r *Repository) FindLiveByValues(ctx context.Context, values []string, currentRound int32) (map[string]domain.Flag, error) {
	const location = "postgres.Repository.FindLiveByValues"

	flags := make(map[string]domain.Flag, len(values))
	if len(values) == 0 {
		return flags, nil
	}
	rows, err := r.queries.GetLiveFlagsByValues(ctx, sqlc.GetLiveFlagsByValuesParams{
		FlagValues:   values,
		CurrentRound: int64(currentRound),
	})
	if err != nil {
		return nil, fmt.Errorf("%s - find live flags by values: %w", location, err)
	}
	for _, row := range rows {
		flag := domainFlag(row)
		flags[flag.Value] = flag
	}
	return flags, nil
}

func (r *Repository) FindRandomRound(
	ctx context.Context,
	teamID, taskID uuid.UUID,
	round int32,
) (domain.Flag, error) {
	const location = "postgres.Repository.FindRandomRound"

	row, err := r.queries.GetRandomRoundFlag(ctx, sqlc.GetRandomRoundFlagParams{
		TeamID: teamID,
		TaskID: taskID,
		Round:  round,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Flag{}, fmt.Errorf("%s - find random round flag: %w", location, domain.ErrFlagNotFound)
	}
	if err != nil {
		return domain.Flag{}, fmt.Errorf("%s - find random round flag: %w", location, err)
	}
	return domainFlag(row), nil
}

func (r *Repository) AttackData(ctx context.Context, currentRound int32, taskIDs []uuid.UUID) ([]domain.AttackDatum, error) {
	const location = "postgres.Repository.AttackData"

	if len(taskIDs) == 0 {
		return []domain.AttackDatum{}, nil
	}
	lifetime, err := r.queries.GetFlagLifetime(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - load flag lifetime: %w", location, err)
	}
	if lifetime <= 0 {
		return nil, fmt.Errorf("%s - validate flag lifetime: %w", location, domain.ErrInvalidGameConfig)
	}
	rows, err := r.queries.GetAttackData(ctx, sqlc.GetAttackDataParams{
		CurrentRound: int64(currentRound),
		FlagLifetime: int64(lifetime),
		TaskIds:      taskIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - load attack data: %w", location, err)
	}
	data := make([]domain.AttackDatum, 0, len(rows))
	for _, row := range rows {
		data = append(data, domain.AttackDatum{
			TaskID:     row.TaskID,
			TeamIP:     row.Ip,
			PublicData: row.PublicFlagData,
		})
	}
	return data, nil
}

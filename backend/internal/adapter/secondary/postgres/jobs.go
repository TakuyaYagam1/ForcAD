package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) Claim(
	ctx context.Context,
	jobID uuid.UUID,
	ownerToken string,
	at, claimDeadline time.Time,
) (domain.CheckerJob, error) {
	const location = "postgres.Repository.Claim"
	if jobID == uuid.Nil() || ownerToken == "" || !claimDeadline.After(at) {
		return domain.CheckerJob{}, fmt.Errorf("%s - validate job claim: %w", location, domain.ErrJobClaimed)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CheckerJob{}, fmt.Errorf("%s - begin claim transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if err := lockJobLifecycle(ctx, queries); err != nil {
		return domain.CheckerJob{}, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	if err := queries.LockGroupedTeamTask(ctx, jobID); err != nil {
		return domain.CheckerJob{}, fmt.Errorf("%s - lock round result: %w", location, err)
	}
	row, err := queries.ClaimCheckerJob(ctx, sqlc.ClaimCheckerJobParams{
		OwnerToken: &ownerToken, ClaimDeadline: timestamp(claimDeadline),
		ClaimedAt: timestamp(at), JobID: jobID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		claimErr := unclaimedJobError(ctx, tx, queries, jobID, at)
		return domain.CheckerJob{}, fmt.Errorf("%s - resolve unavailable job claim: %w", location, claimErr)
	}
	if err != nil {
		return domain.CheckerJob{}, fmt.Errorf("%s - claim checker job: %w", location, err)
	}
	job, err := checkerJobFromClaim(row)
	if err != nil {
		return domain.CheckerJob{}, fmt.Errorf("%s - decode claimed job: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CheckerJob{}, fmt.Errorf("%s - commit job claim: %w", location, err)
	}
	return job, nil
}

func unclaimedJobError(
	ctx context.Context,
	tx pgx.Tx,
	queries *sqlc.Queries,
	jobID uuid.UUID,
	at time.Time,
) error {
	const location = "postgres.unclaimedJobError"
	state, err := queries.GetCheckerJobState(ctx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s - find checker job: %w", location, domain.ErrCheckerJobClosed)
	}
	if err != nil {
		return fmt.Errorf("%s - read checker job state: %w", location, err)
	}
	if state.FinishedAt.Valid {
		return fmt.Errorf("%s - reject completed job: %w", location, domain.ErrCheckerJobClosed)
	}
	if state.DeadlineAt.Valid && !state.DeadlineAt.Time.After(at) {
		return fmt.Errorf("%s - expire overdue claim: %w", location, expireUnclaimedJob(ctx, tx, queries, jobID, at))
	}
	if !state.SentAt.Valid {
		return fmt.Errorf("%s - check job publication: %w", location, domain.ErrJobNotReady)
	}
	return fmt.Errorf("%s - check claim ownership: %w", location, checkUnclaimedJobOwnership(state, at))
}

func expireUnclaimedJob(
	ctx context.Context,
	tx pgx.Tx,
	queries *sqlc.Queries,
	jobID uuid.UUID,
	at time.Time,
) error {
	const location = "postgres.expireUnclaimedJob"
	expired, err := expireCheckerJobInTx(ctx, queries, jobID, at)
	if err != nil {
		return fmt.Errorf("%s - expire overdue job: %w", location, err)
	}
	if !expired {
		return fmt.Errorf("%s - check job readiness: %w", location, domain.ErrJobNotReady)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s - commit expired job: %w", location, err)
	}
	return fmt.Errorf("%s - reject expired job: %w", location, domain.ErrCheckerJobClosed)
}

func checkUnclaimedJobOwnership(state sqlc.GetCheckerJobStateRow, at time.Time) error {
	const location = "postgres.checkUnclaimedJobOwnership"
	workflow, err := checkerJobAction(state.Workflow)
	if err != nil {
		return fmt.Errorf("%s - decode job workflow: %w", location, err)
	}
	claimed := state.ClaimToken != nil && state.ClaimDeadlineAt.Valid && state.ClaimDeadlineAt.Time.After(at)
	putStarted := workflow != domain.JobActionCheckGets && state.PutStartedAt.Valid
	if claimed || putStarted {
		return fmt.Errorf("%s - check job ownership: %w", location, domain.ErrJobClaimed)
	}
	return fmt.Errorf("%s - check job readiness: %w", location, domain.ErrJobNotReady)
}

func (r *Repository) Renew(
	ctx context.Context,
	jobID uuid.UUID,
	ownerToken string,
	at, claimDeadline time.Time,
) (bool, error) {
	const location = "postgres.Repository.Renew"
	if jobID == uuid.Nil() || ownerToken == "" || !claimDeadline.After(at) {
		return false, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin renewal transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if err := lockJobLifecycle(ctx, queries); err != nil {
		if errors.Is(err, domain.ErrGameUnavailable) {
			return false, nil
		}
		return false, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	rows, err := queries.RenewCheckerJob(ctx, sqlc.RenewCheckerJobParams{
		ClaimDeadline: timestamp(claimDeadline), JobID: jobID,
		OwnerToken: &ownerToken, RenewedAt: timestamp(at),
	})
	if err != nil {
		return false, fmt.Errorf("%s - renew checker job lease: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit lease renewal: %w", location, err)
	}
	return rows == 1, nil
}

// ExtendDeadline moves a claimed job's execution deadline forward without
// changing its claim lease. The worker supplies the next action deadline.
func (r *Repository) ExtendDeadline(
	ctx context.Context,
	jobID uuid.UUID,
	ownerToken string,
	at, deadline time.Time,
) (bool, error) {
	const location = "postgres.Repository.ExtendDeadline"
	if jobID == uuid.Nil() || ownerToken == "" || !deadline.After(at) {
		return false, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin deadline transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if err := lockJobLifecycle(ctx, queries); err != nil {
		if errors.Is(err, domain.ErrGameUnavailable) {
			return false, nil
		}
		return false, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	rows, err := queries.ExtendCheckerJobDeadline(ctx, sqlc.ExtendCheckerJobDeadlineParams{
		NewDeadline: timestamp(deadline), JobID: jobID, OwnerToken: &ownerToken,
		TouchedAt: timestamp(at),
	})
	if err != nil {
		return false, fmt.Errorf("%s - extend checker job deadline: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit deadline extension: %w", location, err)
	}
	return rows == 1, nil
}

// SetDeadline assigns a claimed job's execution deadline while preserving the
// claim fence. The claim lease is shortened with the execution deadline when
// needed, so a worker cannot complete after the requested cutoff.
func (r *Repository) SetDeadline(
	ctx context.Context,
	jobID uuid.UUID,
	ownerToken string,
	at, deadline time.Time,
) (bool, error) {
	const location = "postgres.Repository.SetDeadline"
	if jobID == uuid.Nil() || ownerToken == "" || !deadline.After(at) {
		return false, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin deadline transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if err := lockJobLifecycle(ctx, queries); err != nil {
		if errors.Is(err, domain.ErrGameUnavailable) {
			return false, nil
		}
		return false, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	rows, err := queries.SetCheckerJobDeadline(ctx, sqlc.SetCheckerJobDeadlineParams{
		NewDeadline: timestamp(deadline), JobID: jobID, OwnerToken: &ownerToken,
		TouchedAt: timestamp(at),
	})
	if err != nil {
		return false, fmt.Errorf("%s - set checker job deadline: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit job deadline: %w", location, err)
	}
	return rows == 1, nil
}

func (r *Repository) State(ctx context.Context, jobID uuid.UUID) (domain.CheckerJobState, error) {
	const location = "postgres.Repository.State"
	if jobID == uuid.Nil() {
		return domain.CheckerJobState{}, fmt.Errorf("%s - validate job ID: %w", location, domain.ErrCheckerJobClosed)
	}
	row, err := r.queries.GetCheckerJobState(ctx, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CheckerJobState{}, fmt.Errorf("%s - find checker job: %w", location, domain.ErrCheckerJobClosed)
	}
	if err != nil {
		return domain.CheckerJobState{}, fmt.Errorf("%s - read checker job state: %w", location, err)
	}
	workflow, err := checkerJobAction(row.Workflow)
	if err != nil {
		return domain.CheckerJobState{}, fmt.Errorf("%s - decode job workflow: %w", location, err)
	}
	return domain.CheckerJobState{
		Action: workflow, SentAt: nullableTime(row.SentAt),
		FinishedAt: nullableTime(row.FinishedAt), DeadlineAt: nullableTime(row.DeadlineAt),
		ClaimToken: row.ClaimToken, ClaimDeadlineAt: nullableTime(row.ClaimDeadlineAt),
		PutStartedAt: nullableTime(row.PutStartedAt),
	}, nil
}

func (r *Repository) Complete(
	ctx context.Context,
	jobID uuid.UUID,
	ownerToken string,
	at time.Time,
	result domain.CheckerResult,
) (bool, error) {
	const location = "postgres.Repository.Complete"
	if jobID == uuid.Nil() || ownerToken == "" || result.Status <= 0 {
		return false, fmt.Errorf("%s - validate checker result: %w", location, domain.ErrInvalidCheckerResult)
	}
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return false, fmt.Errorf("%s - encode checker result: %w", location, err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin completion transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	completed, err := completeJobAndApply(ctx, queries, jobID, ownerToken, at, result, encodedResult)
	if err != nil {
		return false, err
	}
	if !completed {
		return false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit job completion: %w", location, err)
	}
	return true, nil
}

func completeJobAndApply(
	ctx context.Context,
	queries *sqlc.Queries,
	jobID uuid.UUID,
	ownerToken string,
	at time.Time,
	result domain.CheckerResult,
	encodedResult []byte,
) (bool, error) {
	const location = "postgres.Repository.Complete"
	if err := lockJobLifecycle(ctx, queries); err != nil {
		if errors.Is(err, domain.ErrGameUnavailable) {
			return false, nil
		}
		return false, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	if err := queries.LockGroupedTeamTask(ctx, jobID); err != nil {
		return false, fmt.Errorf("%s - lock round result: %w", location, err)
	}
	row, err := queries.CompleteCheckerJobClaim(ctx, sqlc.CompleteCheckerJobClaimParams{
		FinishedAt: timestamp(at), JobID: jobID, OwnerToken: &ownerToken,
		Result: encodedResult,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s - complete checker job claim: %w", location, err)
	}
	if err := persistCheckerLogs(ctx, queries, row, result.Logs); err != nil {
		return false, fmt.Errorf("%s - persist checker action logs: %w", location, err)
	}
	if err := applyCheckerResult(ctx, queries, row, result); err != nil {
		return false, fmt.Errorf("%s - update task result: %w", location, err)
	}
	return true, nil
}

func persistCheckerLogs(
	ctx context.Context,
	queries *sqlc.Queries,
	row sqlc.CompleteCheckerJobClaimRow,
	logs []domain.CheckerLog,
) error {
	if len(logs) == 0 {
		return nil
	}
	if row.TeamID == nil || row.TaskID == nil {
		return fmt.Errorf("validate checker log team and task: %w", domain.ErrTeamTaskMissing)
	}
	return insertCheckerLogs(ctx, queries, *row.TeamID, *row.TaskID, row.Round, logs)
}

func applyCheckerResult(
	ctx context.Context,
	queries *sqlc.Queries,
	row sqlc.CompleteCheckerJobClaimRow,
	result domain.CheckerResult,
) error {
	const location = "postgres.applyCheckerResult"
	if row.TeamID == nil || row.TaskID == nil {
		return fmt.Errorf("%s - validate job team and task: %w", location, domain.ErrTeamTaskMissing)
	}
	logRound := result.Round
	counted := true
	if row.AggregateRound {
		var ready bool
		var err error
		result, ready, counted, err = groupedResult(ctx, queries, row)
		if err != nil || !ready {
			return err
		}
		logRound = row.Round
	}
	if logRound == 0 {
		logRound = row.Round
	}
	if err := logTaskResult(ctx, queries, logRound, *row.TeamID, *row.TaskID); err != nil {
		return fmt.Errorf("%s - record prior task result: %w", location, err)
	}
	passedDelta := int32(0)
	checkDelta := int32(0)
	if counted {
		checkDelta = 1
	}
	if result.Status == domain.TaskStatusUp {
		passedDelta = 1
	}
	updated, err := queries.UpdateTeamTaskResult(ctx, sqlc.UpdateTeamTaskResultParams{
		Status: &result.Status, PublicMessage: result.PublicMessage,
		PrivateMessage: result.PrivateMessage, Command: result.Command,
		PassedDelta: &passedDelta, CheckDelta: new(checkDelta),
		TeamID: *row.TeamID, TaskID: *row.TaskID,
	})
	if err != nil {
		return fmt.Errorf("%s - update task result: %w", location, err)
	}
	if updated != 1 {
		return fmt.Errorf("%s - find task result row: %w", location, domain.ErrTeamTaskMissing)
	}
	return nil
}

func (r *Repository) Expire(ctx context.Context, jobID uuid.UUID, at time.Time) (bool, error) {
	const location = "postgres.Repository.Expire"
	if jobID == uuid.Nil() {
		return false, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin expiration transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if err := lockJobLifecycle(ctx, queries); err != nil {
		if errors.Is(err, domain.ErrGameUnavailable) {
			return false, nil
		}
		return false, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	expired, err := expireCheckerJobInTx(ctx, queries, jobID, at)
	if err != nil {
		return false, fmt.Errorf("%s - expire checker job: %w", location, err)
	}
	if !expired {
		return false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit job expiration: %w", location, err)
	}
	return true, nil
}

func expireCheckerJobInTx(ctx context.Context, queries *sqlc.Queries, jobID uuid.UUID, at time.Time) (bool, error) {
	const location = "postgres.expireCheckerJobInTx"
	if err := queries.LockGroupedTeamTask(ctx, jobID); err != nil {
		return false, fmt.Errorf("%s - lock round result: %w", location, err)
	}
	row, err := queries.ExpireCheckerJob(ctx, sqlc.ExpireCheckerJobParams{
		FinishedAt: timestamp(at), JobID: jobID,
		Result: []byte(`{"status":110,"public_message":"Checker did not finish","private_message":"Execution or queue deadline expired"}`),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s - expire checker job: %w", location, err)
	}
	if row.TeamID == nil || row.TaskID == nil {
		return false, fmt.Errorf("%s - validate job team and task: %w", location, domain.ErrTeamTaskMissing)
	}
	if row.AggregateRound {
		if err := applyCheckerResult(ctx, queries, sqlc.CompleteCheckerJobClaimRow(row), domain.CheckerResult{}); err != nil {
			return false, fmt.Errorf("%s - finalize round result: %w", location, err)
		}
		return true, nil
	}
	if err := logTaskResult(ctx, queries, row.Round, *row.TeamID, *row.TaskID); err != nil {
		return false, fmt.Errorf("%s - record prior task result: %w", location, err)
	}
	updated, err := queries.UpdateTeamTaskResult(ctx, sqlc.UpdateTeamTaskResultParams{
		Status:        new(domain.TaskStatusCheckFailed),
		PublicMessage: "", PrivateMessage: "", Command: "",
		PassedDelta: new(int32(0)), CheckDelta: new(int32(0)),
		TeamID: *row.TeamID, TaskID: *row.TaskID,
	})
	if err != nil {
		return false, fmt.Errorf("%s - update expired task result: %w", location, err)
	}
	if updated != 1 {
		return false, fmt.Errorf("%s - find task result row: %w", location, domain.ErrTeamTaskMissing)
	}
	return true, nil
}

func (r *Repository) ClaimPending(
	ctx context.Context,
	ownerToken string,
	at, claimDeadline time.Time,
	limit int32,
) ([]domain.CheckerJob, error) {
	const location = "postgres.Repository.ClaimPending"
	if ownerToken == "" || !claimDeadline.After(at) || limit <= 0 {
		return nil, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("%s - begin outbox transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if err := lockJobLifecycle(ctx, queries); err != nil {
		if errors.Is(err, domain.ErrGameUnavailable) {
			return []domain.CheckerJob{}, nil
		}
		return nil, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	rows, err := queries.ClaimOutboxJobs(ctx, sqlc.ClaimOutboxJobsParams{
		OwnerToken: &ownerToken, ClaimDeadline: timestamp(claimDeadline),
		ClaimedAt: timestamp(at), JobLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - claim pending outbox jobs: %w", location, err)
	}
	jobs := make([]domain.CheckerJob, 0, len(rows))
	for _, row := range rows {
		job, err := checkerJobFromOutbox(row)
		if err != nil {
			return nil, fmt.Errorf("%s - decode outbox job: %w", location, err)
		}
		jobs = append(jobs, job)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s - commit outbox claims: %w", location, err)
	}
	return jobs, nil
}

func (r *Repository) MarkPublished(
	ctx context.Context,
	jobID uuid.UUID,
	ownerToken string,
	at, deadline time.Time,
) (bool, error) {
	const location = "postgres.Repository.MarkPublished"
	if jobID == uuid.Nil() || ownerToken == "" || !deadline.After(at) {
		return false, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin publication transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if err := lockJobLifecycle(ctx, queries); err != nil {
		if errors.Is(err, domain.ErrGameUnavailable) {
			return false, nil
		}
		return false, fmt.Errorf("%s - lock job lifecycle: %w", location, err)
	}
	rows, err := queries.MarkOutboxPublished(ctx, sqlc.MarkOutboxPublishedParams{
		PublishedAt: timestamp(at), InitialDeadline: timestamp(deadline),
		JobID: jobID, OwnerToken: &ownerToken,
	})
	if err != nil {
		return false, fmt.Errorf("%s - mark outbox job published: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit job publication: %w", location, err)
	}
	return rows == 1, nil
}

func lockJobLifecycle(ctx context.Context, queries *sqlc.Queries) error {
	const location = "postgres.lockJobLifecycle"
	if _, err := queries.LockGameForAttack(ctx); err != nil {
		return fmt.Errorf("%s - lock game configuration: %w", location, err)
	}
	session, err := queries.LockSessionForAttack(ctx)
	if err != nil {
		return fmt.Errorf("%s - lock game session: %w", location, err)
	}
	if session.ResetPending {
		return fmt.Errorf("%s - check pending session reset: %w", location, domain.ErrGameUnavailable)
	}
	return nil
}

func logTaskResult(ctx context.Context, queries *sqlc.Queries, round int32, teamID, taskID uuid.UUID) error {
	const location = "postgres.logTaskResult"
	rows, err := queries.LogCurrentTeamTask(ctx, sqlc.LogCurrentTeamTaskParams{
		LogRound: &round, TeamID: teamID, TaskID: taskID,
	})
	if err != nil {
		return fmt.Errorf("%s - record prior task result: %w", location, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s - find task result row: %w", location, domain.ErrTeamTaskMissing)
	}
	return nil
}

var errInvalidCheckerJobIdentity = errors.New("checker job identity is invalid")

func checkerJobAction(action *string) (domain.JobAction, error) {
	const location = "postgres.checkerJobAction"
	if action == nil {
		return "", fmt.Errorf("%s - require job workflow: %w", location, domain.ErrInvalidJobWorkflow)
	}
	workflow := domain.JobAction(*action)
	switch workflow {
	case domain.JobActionClassic, domain.JobActionPutsOnly, domain.JobActionCheckGets, domain.JobActionCheckOnly:
		return workflow, nil
	default:
		return "", fmt.Errorf("%s - validate job workflow: %w", location, domain.ErrInvalidJobWorkflow)
	}
}

func checkerJobFromClaim(row sqlc.ClaimCheckerJobRow) (domain.CheckerJob, error) {
	job, err := makeCheckerJob(
		row.ID, row.RunID, row.Round, row.Action, row.TeamID, row.TaskID, row.Payload,
		row.SentAt, row.FinishedAt, row.DeadlineAt, row.PublishToken,
		row.PublishDeadlineAt, row.ClaimToken, row.ClaimDeadlineAt, row.PutStartedAt,
	)
	job.NotBefore, job.ExpiresAt = nullableTime(row.NotBefore), nullableTime(row.ExpiresAt)
	return job, err
}

func checkerJobFromOutbox(row sqlc.ClaimOutboxJobsRow) (domain.CheckerJob, error) {
	job, err := makeCheckerJob(
		row.ID, row.RunID, row.Round, row.Action, row.TeamID, row.TaskID, row.Payload,
		row.SentAt, row.FinishedAt, row.DeadlineAt, row.PublishToken,
		row.PublishDeadlineAt, row.ClaimToken, row.ClaimDeadlineAt, row.PutStartedAt,
	)
	job.NotBefore, job.ExpiresAt = nullableTime(row.NotBefore), nullableTime(row.ExpiresAt)
	return job, err
}

func makeCheckerJob(
	id, runID uuid.UUID,
	round int32,
	action *string,
	teamID, taskID *uuid.UUID,
	payload []byte,
	sentAt, finishedAt, deadlineAt pgtype.Timestamptz,
	publishToken *string,
	publishDeadlineAt pgtype.Timestamptz,
	claimToken *string,
	claimDeadlineAt, putStartedAt pgtype.Timestamptz,
) (domain.CheckerJob, error) {
	const location = "postgres.makeCheckerJob"
	if id == uuid.Nil() || runID == uuid.Nil() || round < 0 {
		return domain.CheckerJob{}, fmt.Errorf("%s - validate job identity: %w", location, errInvalidCheckerJobIdentity)
	}
	jobAction, err := checkerJobAction(action)
	if err != nil {
		return domain.CheckerJob{}, fmt.Errorf("%s - decode job workflow: %w", location, err)
	}
	if teamID == nil || taskID == nil || *teamID == uuid.Nil() || *taskID == uuid.Nil() {
		return domain.CheckerJob{}, fmt.Errorf("%s - validate job team and task: %w", location, domain.ErrTeamTaskMissing)
	}
	return domain.CheckerJob{
		ID: id, RunID: runID, Round: round, Action: jobAction,
		TeamID: *teamID, TaskID: *taskID, Payload: rawJSON(payload),
		SentAt: nullableTime(sentAt), FinishedAt: nullableTime(finishedAt),
		DeadlineAt: nullableTime(deadlineAt), PublishToken: publishToken,
		PublishDeadlineAt: nullableTime(publishDeadlineAt), ClaimToken: claimToken,
		ClaimDeadlineAt: nullableTime(claimDeadlineAt), PutStartedAt: nullableTime(putStartedAt),
	}, nil
}

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
	"github.com/jackc/pgx/v5"
)

var _ outbound.SchedulerRepository = (*Repository)(nil)

func (r *Repository) Prepare(ctx context.Context, request domain.ScheduleRequest) (*domain.DispatchRun, error) {
	const location = "postgres.Repository.Prepare"
	if err := validateScheduleRequest(request); err != nil {
		return nil, fmt.Errorf("%s - validate scheduler request: %w", location, err)
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("%s - begin schedule transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	resolved, err := resolveSchedule(ctx, tx, queries, request)
	if err != nil {
		return nil, fmt.Errorf("%s - resolve schedule: %w", location, err)
	}
	if resolved.done {
		return resolved.run, nil
	}

	roundState, stop, err := prepareSchedulerRound(ctx, queries, request, resolved.game, resolved.session)
	if err != nil {
		return nil, fmt.Errorf("%s - prepare round state: %w", location, err)
	}
	if stop {
		commitMessage := "commit blocked round check"
		if resolved.game.Rounds != nil && resolved.game.RealRound >= *resolved.game.Rounds && request.AdvancesRound {
			commitMessage = "commit round limit update"
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("%s - %s: %w", location, commitMessage, err)
		}
		return nil, nil
	}
	run, err := createSchedulerRun(ctx, queries, request, resolved.logicalAt, roundState, resolved.session.Generation, resolved.game)
	if err != nil {
		return nil, fmt.Errorf("%s - create dispatch run: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s - commit schedule transaction: %w", location, err)
	}
	return run, nil
}

type schedulerRoundState struct {
	round             int32
	snapshot          *domain.RoundSnapshot
	cellsSnapshot     []byte
	snapshotRound     *int32
	snapshotRoundTime *float64
}

type scheduleResolution struct {
	game      sqlc.LockSchedulerGameRow
	session   sqlc.LockSchedulerSessionRow
	logicalAt time.Time
	run       *domain.DispatchRun
	done      bool
}

func resolveSchedule(
	ctx context.Context,
	tx pgx.Tx,
	queries *sqlc.Queries,
	request domain.ScheduleRequest,
) (scheduleResolution, error) {
	const location = "postgres.resolveSchedule"
	game, session, err := lockScheduleState(ctx, queries)
	if err != nil {
		return scheduleResolution{}, fmt.Errorf("%s - lock scheduling state: %w", location, err)
	}
	if !game.GameRunning || session.ResetPending || session.PausedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return scheduleResolution{}, fmt.Errorf("%s - commit inactive schedule check: %w", location, err)
		}
		return scheduleResolution{done: true}, nil
	}
	run, found, err := pendingScheduleRun(ctx, queries, request.ScheduleID, session.Generation)
	if err != nil {
		return scheduleResolution{}, fmt.Errorf("%s - load pending schedule run: %w", location, err)
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return scheduleResolution{}, fmt.Errorf("%s - commit pending dispatch lookup: %w", location, err)
		}
		return scheduleResolution{run: run, done: true}, nil
	}
	logicalAt := request.At.Add(-time.Duration(session.SchedulePausedSeconds * float64(time.Second)))
	due, err := scheduleIsDue(ctx, queries, request, logicalAt)
	if err != nil {
		return scheduleResolution{}, fmt.Errorf("%s - check schedule due time: %w", location, err)
	}
	if !due {
		if err := tx.Commit(ctx); err != nil {
			return scheduleResolution{}, fmt.Errorf("%s - commit not-due schedule check: %w", location, err)
		}
		return scheduleResolution{done: true}, nil
	}
	return scheduleResolution{game: game, session: session, logicalAt: logicalAt}, nil
}

func validateScheduleRequest(request domain.ScheduleRequest) error {
	const location = "postgres.validateScheduleRequest"
	workflow := request.Workflow
	if request.ScheduleID == "" || request.At.IsZero() || request.StartAt.IsZero() || request.Interval < 0 ||
		!validJobAction(workflow) ||
		(workflow == domain.JobActionCheckGets && request.TaskID == nil) ||
		(request.TaskID != nil && *request.TaskID == uuid.Nil()) {
		return fmt.Errorf("%s - validate scheduler request", location)
	}
	return nil
}

func lockScheduleState(ctx context.Context, queries *sqlc.Queries) (sqlc.LockSchedulerGameRow, sqlc.LockSchedulerSessionRow, error) {
	const location = "postgres.lockScheduleState"
	game, err := queries.LockSchedulerGame(ctx)
	if err != nil {
		return sqlc.LockSchedulerGameRow{}, sqlc.LockSchedulerSessionRow{}, fmt.Errorf("%s - lock game for scheduling: %w", location, err)
	}
	session, err := queries.LockSchedulerSession(ctx)
	if err != nil {
		return sqlc.LockSchedulerGameRow{}, sqlc.LockSchedulerSessionRow{}, fmt.Errorf("%s - lock session for scheduling: %w", location, err)
	}
	return game, session, nil
}

func pendingScheduleRun(
	ctx context.Context,
	queries *sqlc.Queries,
	scheduleID string,
	generation int64,
) (*domain.DispatchRun, bool, error) {
	const location = "postgres.pendingScheduleRun"
	row, err := queries.PendingSchedulerRun(ctx, scheduleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%s - look up pending dispatch run: %w", location, err)
	}
	run, err := dispatchRunFromRow(row, generation)
	if err != nil {
		return nil, false, fmt.Errorf("%s - load pending dispatch run: %w", location, err)
	}
	return run, true, nil
}

func scheduleIsDue(
	ctx context.Context,
	queries *sqlc.Queries,
	request domain.ScheduleRequest,
	logicalAt time.Time,
) (bool, error) {
	const location = "postgres.scheduleIsDue"
	due, err := queries.IsSchedulerScheduleDue(ctx, sqlc.IsSchedulerScheduleDueParams{
		ScheduleAt: timestamp(logicalAt), StartAt: timestamp(request.StartAt),
		IntervalSeconds: request.Interval.Seconds(), ScheduleID: request.ScheduleID,
	})
	if err != nil {
		return false, fmt.Errorf("%s - check whether schedule is due: %w", location, err)
	}
	return due != nil && *due, nil
}

func prepareSchedulerRound(
	ctx context.Context,
	queries *sqlc.Queries,
	request domain.ScheduleRequest,
	game sqlc.LockSchedulerGameRow,
	session sqlc.LockSchedulerSessionRow,
) (schedulerRoundState, bool, error) {
	const location = "postgres.prepareSchedulerRound"
	state := schedulerRoundState{round: game.RealRound}
	if !request.AdvancesRound {
		return state, false, nil
	}
	canAdvance, err := schedulerCanAdvanceRound(ctx, queries, game)
	if err != nil {
		return schedulerRoundState{}, false, fmt.Errorf("%s - check round advancement: %w", location, err)
	}
	if !canAdvance {
		return state, true, nil
	}
	state, err = completedSchedulerRound(ctx, queries, game.RealRound, session)
	if err != nil {
		return schedulerRoundState{}, false, fmt.Errorf("%s - snapshot completed round: %w", location, err)
	}
	if err := advanceSchedulerRound(ctx, queries, request.At, game.RealRound, state.snapshot); err != nil {
		return schedulerRoundState{}, false, fmt.Errorf("%s - persist new round: %w", location, err)
	}
	state.round = game.RealRound + 1
	return state, false, nil
}

func schedulerCanAdvanceRound(
	ctx context.Context,
	queries *sqlc.Queries,
	game sqlc.LockSchedulerGameRow,
) (bool, error) {
	const location = "postgres.schedulerCanAdvanceRound"
	if game.Rounds != nil && game.RealRound >= *game.Rounds {
		if _, err := queries.StopAtRoundLimit(ctx); err != nil {
			return false, fmt.Errorf("%s - stop game at round limit: %w", location, err)
		}
		return false, nil
	}
	blocked, err := queries.HasSchedulerJobsThroughRound(ctx, game.RealRound)
	if err != nil {
		return false, fmt.Errorf("%s - check outstanding round jobs: %w", location, err)
	}
	return !blocked, nil
}

func completedSchedulerRound(
	ctx context.Context,
	queries *sqlc.Queries,
	currentRound int32,
	session sqlc.LockSchedulerSessionRow,
) (schedulerRoundState, error) {
	const location = "postgres.completedSchedulerRound"
	encodedCells, err := queries.GetSchedulerTeamTaskSnapshot(ctx)
	if err != nil {
		return schedulerRoundState{}, fmt.Errorf("%s - load completed round cells: %w", location, err)
	}
	cellsSnapshot := []byte(encodedCells)
	cells, err := decodeSnapshotCells(cellsSnapshot)
	if err != nil {
		return schedulerRoundState{}, fmt.Errorf("%s - decode completed round cells: %w", location, err)
	}
	completedStart := int64(0)
	if session.RoundStartedAt.Valid {
		completedStart = session.RoundStartedAt.Time.Unix()
	}
	snapshot := publicRoundSnapshot(currentRound, completedStart, cells)
	if currentRound > 0 {
		if err := insertRoundSnapshot(ctx, queries, *snapshot); err != nil {
			return schedulerRoundState{}, fmt.Errorf("%s - save completed round snapshot: %w", location, err)
		}
	}
	snapshotStart := float64(completedStart)
	return schedulerRoundState{
		snapshot: snapshot, cellsSnapshot: cellsSnapshot,
		snapshotRound: &currentRound, snapshotRoundTime: &snapshotStart,
	}, nil
}

func advanceSchedulerRound(
	ctx context.Context,
	queries *sqlc.Queries,
	at time.Time,
	currentRound int32,
	snapshot *domain.RoundSnapshot,
) error {
	const location = "postgres.advanceSchedulerRound"
	newRound := currentRound + 1
	if rows, err := queries.SetSchedulerRound(ctx, &newRound); err != nil {
		return fmt.Errorf("%s - advance game round: %w", location, err)
	} else if rows != 1 {
		return fmt.Errorf("%s - advance game round: %w", location, domain.ErrGameUnavailable)
	}
	encodedSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("%s - encode completed round snapshot: %w", location, err)
	}
	if rows, err := queries.SaveSchedulerRoundRuntime(ctx, sqlc.SaveSchedulerRoundRuntimeParams{
		RoundStartedAt: timestamp(at), SnapshotRound: &currentRound, Snapshot: encodedSnapshot,
	}); err != nil {
		return fmt.Errorf("%s - save current round runtime: %w", location, err)
	} else if rows != 1 {
		return fmt.Errorf("%s - save current round runtime: %w", location, domain.ErrGameUnavailable)
	}
	return nil
}

func createSchedulerRun(
	ctx context.Context,
	queries *sqlc.Queries,
	request domain.ScheduleRequest,
	logicalAt time.Time,
	state schedulerRoundState,
	generation int64,
	game sqlc.LockSchedulerGameRow,
) (*domain.DispatchRun, error) {
	const location = "postgres.createSchedulerRun"
	runID := uuid.NewV7()
	roundStart := float64(request.At.UnixNano()) / float64(time.Second)
	runRow, err := queries.CreateSchedulerRun(ctx, sqlc.CreateSchedulerRunParams{
		ID: runID, ScheduleID: request.ScheduleID, Round: state.round,
		IsRound: request.AdvancesRound, RoundStart: roundStart,
		Snapshot: state.cellsSnapshot, SnapshotRound: state.snapshotRound,
		SnapshotRoundStart: state.snapshotRoundTime, ScheduledAt: timestamp(logicalAt),
	})
	if err != nil {
		return nil, fmt.Errorf("%s - create dispatch run: %w", location, err)
	}
	teams, err := queries.ListGameTeams(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("%s - list active teams: %w", location, err)
	}
	tasks, err := queries.ListGameTasks(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("%s - list active tasks: %w", location, err)
	}
	jobs, err := buildSchedulerJobs(runID, state.round, generation, request.Workflow, request.TaskID, teams, tasks)
	if err != nil {
		return nil, fmt.Errorf("%s - build checker jobs: %w", location, err)
	}
	if game.ExtraChecks > 0 {
		if !request.AdvancesRound || request.Workflow != domain.JobActionClassic {
			return nil, fmt.Errorf("%s - extra checks require classic round scheduling", location)
		}
		jobs, err = addExtraChecks(jobs, request.At, game.RoundTime, game.ExtraChecks, rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("%s - schedule extra checks: %w", location, err)
		}
	}
	if err := shuffleSchedulerJobs(jobs, rand.Reader); err != nil {
		return nil, fmt.Errorf("%s - shuffle checker jobs: %w", location, err)
	}
	if len(jobs) > 0 {
		encodedJobs, err := json.Marshal(jobs)
		if err != nil {
			return nil, fmt.Errorf("%s - encode checker jobs: %w", location, err)
		}
		if err := queries.InsertSchedulerJobs(ctx, encodedJobs); err != nil {
			return nil, fmt.Errorf("%s - insert checker jobs: %w", location, err)
		}
	}
	run, err := dispatchRunFromRow(runRow, generation)
	if err != nil {
		return nil, fmt.Errorf("%s - build dispatch run result: %w", location, err)
	}
	run.Workflow = request.Workflow
	if state.snapshot != nil {
		run.Snapshot = state.snapshot
	}
	return run, nil
}

func (r *Repository) Finish(ctx context.Context, runID uuid.UUID, at time.Time) (bool, error) {
	const location = "postgres.Repository.Finish"
	if runID == uuid.Nil() || at.IsZero() {
		return false, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin dispatch completion transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	if _, err := queries.LockGameForSession(ctx); err != nil {
		return false, fmt.Errorf("%s - lock game for dispatch completion: %w", location, err)
	}
	if _, err := queries.LockSessionForUpdate(ctx); err != nil {
		return false, fmt.Errorf("%s - lock session for dispatch completion: %w", location, err)
	}
	completedRun, err := queries.FinishDispatchRun(ctx, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s - finish dispatch run: %w", location, err)
	}
	if !completedRun.ScheduledAt.Valid {
		return false, fmt.Errorf("%s - dispatch %s is missing scheduled time", location, runID)
	}
	if err := queries.SaveScheduleCheckpoint(ctx, sqlc.SaveScheduleCheckpointParams{
		ScheduleID: completedRun.ScheduleID, LastRun: completedRun.ScheduledAt,
	}); err != nil {
		return false, fmt.Errorf("%s - save schedule checkpoint: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit dispatch completion transaction: %w", location, err)
	}
	return true, nil
}

// StartScheduled atomically starts a due final session only while the game is
// still in its untouched waiting state. The session marker makes a finished
// final terminal even when it ended before the first round was dispatched.
func (r *Repository) StartScheduled(ctx context.Context, at time.Time) (bool, error) {
	const location = "postgres.Repository.StartScheduled"
	if at.IsZero() {
		return false, fmt.Errorf("%s - validate scheduled start timestamp", location)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin scheduled start transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	game, err := queries.LockSchedulerGame(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - lock game for scheduled start: %w", location, err)
	}
	session, err := queries.LockSchedulerSession(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - lock session for scheduled start: %w", location, err)
	}
	if !scheduledStartEligible(game, session, at) {
		// Release read locks through rollback; an idle poll needs no WAL flush.
		return false, nil
	}
	running, err := queries.StartScheduledGame(ctx, timestamp(at))
	if err != nil {
		return false, fmt.Errorf("%s - start scheduled game: %w", location, err)
	}
	if running != 1 {
		return false, nil
	}
	started, err := queries.StartScheduledFinalSession(ctx, game.StartTime)
	if err != nil {
		return false, fmt.Errorf("%s - start scheduled final session: %w", location, err)
	}
	if started != 1 {
		return false, fmt.Errorf("%s - start scheduled final session: %w", location, domain.ErrGameUnavailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit scheduled start transaction: %w", location, err)
	}
	return true, nil
}

func scheduledStartEligible(
	game sqlc.LockSchedulerGameRow,
	session sqlc.LockSchedulerSessionRow,
	at time.Time,
) bool {
	return !game.GameRunning && game.RealRound == 0 && game.StartTime.Valid && !at.Before(game.StartTime.Time) &&
		!session.PracticeStart.Valid && !session.FinalStart.Valid && !session.ResetPending && !session.PausedAt.Valid
}

func (r *Repository) Maintain(ctx context.Context, at time.Time) (bool, error) {
	const location = "postgres.Repository.Maintain"
	if at.IsZero() {
		return false, fmt.Errorf("%s - validate maintenance timestamp", location)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("%s - begin maintenance transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	queries := r.queries.WithTx(tx)
	game, err := queries.LockSchedulerGame(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - lock game for maintenance: %w", location, err)
	}
	session, err := queries.LockSchedulerSession(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - lock session for maintenance: %w", location, err)
	}

	if practiceCleanupDue(game, session, at) {
		maintained, err := maintainPracticeSession(ctx, tx, queries, game, at)
		if err != nil {
			return false, fmt.Errorf("%s - clean rehearsal state: %w", location, err)
		}
		return maintained, nil
	}
	maintained, err := maintainCompletedRound(ctx, tx, queries, game, session)
	if err != nil {
		return false, fmt.Errorf("%s - maintain completed round: %w", location, err)
	}
	return maintained, nil
}

func practiceCleanupDue(
	game sqlc.LockSchedulerGameRow,
	session sqlc.LockSchedulerSessionRow,
	at time.Time,
) bool {
	if session.ResetPending {
		return true
	}
	return session.PracticeStart.Valid &&
		(!game.GameRunning || game.StartTime.Valid && !at.Before(game.StartTime.Time))
}

func maintainPracticeSession(
	ctx context.Context,
	tx pgx.Tx,
	queries *sqlc.Queries,
	game sqlc.LockSchedulerGameRow,
	at time.Time,
) (bool, error) {
	const location = "postgres.maintainPracticeSession"
	if game.GameRunning {
		if err := queries.StopPracticeSession(ctx); err != nil {
			return false, fmt.Errorf("%s - stop rehearsal session: %w", location, err)
		}
	}
	if err := queries.CancelPracticeOutbox(ctx, timestamp(at)); err != nil {
		return false, fmt.Errorf("%s - cancel rehearsal outbox: %w", location, err)
	}
	pending, err := queries.HasUnfinishedSchedulerJobs(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - check unfinished rehearsal jobs: %w", location, err)
	}
	if pending {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("%s - commit pending rehearsal cleanup check: %w", location, err)
		}
		return false, nil
	}
	if err := queries.MarkPracticeResetPending(ctx); err != nil {
		return false, fmt.Errorf("%s - mark rehearsal reset pending: %w", location, err)
	}
	for _, clear := range []struct {
		operation string
		apply     func(context.Context) error
	}{
		{"delete rehearsal jobs", queries.DeletePracticeJobs},
		{"delete rehearsal dispatch runs", queries.DeletePracticeRuns},
		{"delete rehearsal stolen flags", queries.DeletePracticeStolenFlags},
		{"delete rehearsal flags", queries.DeletePracticeFlags},
		{"delete rehearsal history", queries.DeletePracticeHistory},
		{"delete rehearsal runtime", queries.DeletePracticeRuntime},
		{"delete rehearsal schedule history", queries.DeletePracticeScheduleHistory},
		{"reset rehearsal team tasks", queries.ResetPracticeTeamTasks},
		{"reset rehearsal game", queries.ResetPracticeGame},
		{"reset rehearsal session", queries.ResetPracticeSession},
	} {
		if err := clear.apply(ctx); err != nil {
			return false, fmt.Errorf("%s - %s: %w", location, clear.operation, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit rehearsal reset transaction: %w", location, err)
	}
	return true, nil
}

func maintainCompletedRound(
	ctx context.Context,
	tx pgx.Tx,
	queries *sqlc.Queries,
	game sqlc.LockSchedulerGameRow,
	session sqlc.LockSchedulerSessionRow,
) (bool, error) {
	const location = "postgres.maintainCompletedRound"
	if game.GameRunning || game.RealRound < 1 || session.ResetPending || session.PracticeStart.Valid {
		return false, nil
	}
	pending, err := queries.HasUnfinishedSchedulerJobs(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - check unfinished game jobs: %w", location, err)
	}
	if pending {
		return false, nil
	}
	if session.SnapshotRound != nil && *session.SnapshotRound == game.RealRound && len(session.Snapshot) > 0 {
		return false, nil
	}
	if err := saveLatestCompletedRound(ctx, queries, game.RealRound, session); err != nil {
		return false, fmt.Errorf("%s - save latest completed round: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit maintenance transaction: %w", location, err)
	}
	return true, nil
}

func (r *Repository) ExpiredJobIDs(ctx context.Context, at time.Time, limit int32) ([]uuid.UUID, error) {
	const location = "postgres.Repository.ExpiredJobIDs"
	if at.IsZero() || limit <= 0 {
		return []uuid.UUID{}, nil
	}
	ids, err := r.queries.ExpiredSchedulerJobs(ctx, sqlc.ExpiredSchedulerJobsParams{
		At: timestamp(at), JobLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - list expired scheduler jobs: %w", location, err)
	}
	return ids, nil
}

func (r *Repository) CompletedSnapshots(ctx context.Context, afterRound, limit int32) ([]domain.RoundSnapshot, error) {
	const location = "postgres.Repository.CompletedSnapshots"
	if limit <= 0 {
		return []domain.RoundSnapshot{}, nil
	}
	rows, err := r.queries.ListCompletedRoundSnapshots(ctx, sqlc.ListCompletedRoundSnapshotsParams{
		AfterRound: afterRound, SnapshotLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - list completed round snapshots: %w", location, err)
	}
	snapshots := make([]domain.RoundSnapshot, 0, len(rows))
	for _, row := range rows {
		var snapshot domain.RoundSnapshot
		if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil {
			return nil, fmt.Errorf("%s - decode completed round %d snapshot: %w", location, row.Round, err)
		}
		snapshot.Round = row.Round
		snapshot.RoundStart = int64(row.RoundStart)
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func (r *Repository) LastRun(ctx context.Context, scheduleID string) (*time.Time, error) {
	const location = "postgres.Repository.LastRun"
	if scheduleID == "" {
		return nil, nil
	}
	stamp, err := r.queries.GetSchedulerLastRun(ctx, scheduleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s - load schedule checkpoint: %w", location, err)
	}
	value := stamp.Time
	return &value, nil
}

type schedulerJobRecord struct {
	ID             uuid.UUID       `json:"id"`
	RunID          uuid.UUID       `json:"run_id"`
	Round          int32           `json:"round"`
	Payload        json.RawMessage `json:"payload"`
	Action         string          `json:"action"`
	TeamID         uuid.UUID       `json:"team_id"`
	TaskID         uuid.UUID       `json:"task_id"`
	NotBefore      *time.Time      `json:"not_before"`
	ExpiresAt      *time.Time      `json:"expires_at"`
	AggregateRound bool            `json:"aggregate_round"`
}

func buildSchedulerJobs(
	runID uuid.UUID,
	round int32,
	generation int64,
	workflow domain.JobAction,
	taskID *uuid.UUID,
	teams []sqlc.ListGameTeamsRow,
	tasks []sqlc.ListGameTasksRow,
) ([]schedulerJobRecord, error) {
	const location = "postgres.buildSchedulerJobs"
	jobs := make([]schedulerJobRecord, 0, len(teams)*len(tasks))
	for _, teamRow := range teams {
		team := domain.Team{
			ID: teamRow.ID, Name: teamRow.Name, IP: teamRow.Ip, Token: teamRow.Token,
			Highlighted: teamRow.Highlighted, Active: teamRow.Active, LogoPath: teamRow.LogoPath,
		}
		for _, taskRow := range tasks {
			if taskID != nil && taskRow.ID != *taskID {
				continue
			}
			if workflow == domain.JobActionPutsOnly && taskRow.Puts == 0 {
				continue
			}
			task := domain.Task{
				ID: taskRow.ID, Name: taskRow.Name, Checker: taskRow.Checker, EnvPath: taskRow.EnvPath,
				Gets: taskRow.Gets, Puts: taskRow.Puts, Places: taskRow.Places,
				CheckerTimeout: taskRow.CheckerTimeout, CheckerType: taskRow.CheckerType,
				GetPeriod: taskRow.GetPeriod, DefaultScore: taskRow.DefaultScore, Active: taskRow.Active,
			}
			payload, err := json.Marshal(domain.JobPayload{
				Team: team, Task: task, Workflow: string(workflow), Generation: generation,
			})
			if err != nil {
				return nil, fmt.Errorf("%s - encode checker job payload: %w", location, err)
			}
			jobs = append(jobs, schedulerJobRecord{
				RunID: runID, Round: round, Payload: payload,
				Action: string(workflow), TeamID: team.ID, TaskID: task.ID,
			})
		}
	}
	return jobs, nil
}

func shuffleSchedulerJobs(jobs []schedulerJobRecord, random io.Reader) error {
	const location = "postgres.shuffleSchedulerJobs"
	for i := len(jobs) - 1; i > 0; i-- {
		index, err := rand.Int(random, big.NewInt(int64(i+1)))
		if err != nil {
			return fmt.Errorf("%s - choose job position: %w", location, err)
		}
		jobs[i], jobs[index.Int64()] = jobs[index.Int64()], jobs[i]
	}
	for i := range jobs {
		jobs[i].ID = uuid.NewV7()
	}
	return nil
}

func dispatchRunFromRow(row sqlc.Dispatchrun, generation int64) (*domain.DispatchRun, error) {
	const location = "postgres.dispatchRunFromRow"
	if !row.ScheduledAt.Valid {
		return nil, fmt.Errorf("%s - dispatch %s is missing scheduled time", location, row.ID)
	}
	run := &domain.DispatchRun{
		ID: row.ID, ScheduleID: row.ScheduleID, Round: row.Round,
		IsRound: row.IsRound, RoundStart: row.RoundStart,
		Completed: row.Completed, Generation: generation,
		Workflow: workflowFromSchedule(row.ScheduleID),
	}
	run.ScheduledAt = row.ScheduledAt.Time
	if !row.IsRound {
		return run, nil
	}
	if len(row.Snapshot) == 0 || string(row.Snapshot) == "null" {
		return nil, fmt.Errorf("%s - dispatch %s is missing snapshot data", location, row.ID)
	}
	if row.SnapshotRound == nil || row.SnapshotRoundStart == nil {
		return nil, fmt.Errorf("%s - dispatch %s is missing snapshot metadata", location, row.ID)
	}
	var cells []domain.Cell
	if err := json.Unmarshal(row.Snapshot, &cells); err != nil {
		return nil, fmt.Errorf("%s - decode dispatch %s snapshot: %w", location, row.ID, err)
	}
	run.Snapshot = publicRoundSnapshot(*row.SnapshotRound, int64(*row.SnapshotRoundStart), cells)
	return run, nil
}

func workflowFromSchedule(scheduleID string) domain.JobAction {
	if scheduleID == "blitz_rounds" {
		return domain.JobActionPutsOnly
	}
	if strings.HasPrefix(scheduleID, "blitz_check_gets_task_") {
		return domain.JobActionCheckGets
	}
	return domain.JobActionClassic
}

func decodeSnapshotCells(encoded []byte) ([]domain.Cell, error) {
	const location = "postgres.decodeSnapshotCells"
	var cells []domain.Cell
	if err := json.Unmarshal(encoded, &cells); err != nil {
		return nil, fmt.Errorf("%s - decode team task snapshot: %w", location, err)
	}
	return cells, nil
}

func publicRoundSnapshot(round int32, roundStart int64, cells []domain.Cell) *domain.RoundSnapshot {
	publicCells := make([]domain.PublicCell, 0, len(cells))
	for _, cell := range cells {
		publicCells = append(publicCells, cell.Public())
	}
	return &domain.RoundSnapshot{Round: round, RoundStart: roundStart, TeamTasks: publicCells}
}

func insertRoundSnapshot(ctx context.Context, queries *sqlc.Queries, snapshot domain.RoundSnapshot) error {
	const location = "postgres.insertRoundSnapshot"
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("%s - encode completed round snapshot: %w", location, err)
	}
	if _, err := queries.InsertCompletedRoundSnapshot(ctx, sqlc.InsertCompletedRoundSnapshotParams{
		Round: snapshot.Round, RoundStart: float64(snapshot.RoundStart), Snapshot: encoded,
	}); err != nil {
		return fmt.Errorf("%s - insert completed round snapshot: %w", location, err)
	}
	return nil
}

func saveLatestCompletedRound(
	ctx context.Context,
	queries *sqlc.Queries,
	round int32,
	session sqlc.LockSchedulerSessionRow,
) error {
	const location = "postgres.saveLatestCompletedRound"
	encodedCells, err := queries.GetSchedulerTeamTaskSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("%s - load completed round cells: %w", location, err)
	}
	cells, err := decodeSnapshotCells([]byte(encodedCells))
	if err != nil {
		return fmt.Errorf("%s - decode completed round cells: %w", location, err)
	}
	roundStart := int64(0)
	if session.RoundStartedAt.Valid {
		roundStart = session.RoundStartedAt.Time.Unix()
	}
	snapshot := publicRoundSnapshot(round, roundStart, cells)
	if err := insertRoundSnapshot(ctx, queries, *snapshot); err != nil {
		return fmt.Errorf("%s - insert completed round snapshot: %w", location, err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("%s - encode completed round snapshot: %w", location, err)
	}
	rows, err := queries.SaveGameSnapshot(ctx, sqlc.SaveGameSnapshotParams{
		SnapshotRound: &round, Snapshot: encoded,
	})
	if err != nil {
		return fmt.Errorf("%s - update current game snapshot: %w", location, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s - update current game snapshot: %w", location, domain.ErrGameUnavailable)
	}
	return nil
}

func validJobAction(action domain.JobAction) bool {
	switch action {
	case domain.JobActionClassic, domain.JobActionPutsOnly, domain.JobActionCheckGets:
		return true
	case domain.JobActionCheckOnly:
		// Extra CHECKs belong to an existing classic round, not a separate schedule.
		return false
	default:
		return false
	}
}

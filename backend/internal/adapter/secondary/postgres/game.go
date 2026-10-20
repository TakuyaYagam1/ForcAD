package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type GameStore struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

const transactionCleanupTimeout = time.Second

var _ outbound.GameRepository = (*GameStore)(nil)

func NewGameStore(pool *pgxpool.Pool) *GameStore {
	return &GameStore{pool: pool, queries: sqlc.New(pool)}
}

func gameTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return new(t.Time)
}
func gameStamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func readLifecycle(ctx context.Context, q *sqlc.Queries, withSnapshot bool) (domain.Lifecycle, error) {
	const location = "postgres.readLifecycle"
	row, err := q.ReadGameLifecycle(ctx, withSnapshot)
	if err != nil {
		return domain.Lifecycle{}, fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	return domain.Lifecycle{
		Config:  domain.GameConfig{ID: row.ID, GameRunning: row.GameRunning, RealRound: row.RealRound, FlagLifetime: row.FlagLifetime, GameHardness: row.GameHardness, Inflation: row.Inflation, VolgaAttacksMode: row.VolgaAttacksMode, RoundTime: row.RoundTime, ExtraChecks: row.ExtraChecks, Rounds: row.Rounds, Mode: row.Mode, Timezone: row.Timezone, StartTime: row.StartTime.Time},
		Session: domain.Session{PracticeStart: gameTime(row.PracticeStart), FinalStart: gameTime(row.FinalStart), ResetPending: row.ResetPending, Generation: row.Generation, PausedAt: gameTime(row.PausedAt), PausedSeconds: row.PausedSeconds, SchedulePausedSeconds: row.SchedulePausedSeconds, RoundStartedAt: gameTime(row.RoundStartedAt), SnapshotRound: row.SnapshotRound, Snapshot: row.Snapshot},
		Pending: row.Pending,
	}, nil
}

func (g *GameStore) Lifecycle(ctx context.Context) (domain.Lifecycle, error) {
	// A single SELECT gives one MVCC snapshot without holding a pooled
	// connection across BEGIN, three reads and COMMIT. The board alone needs
	// the potentially large completed-round JSON snapshot.
	return readLifecycle(ctx, g.queries, false)
}

func (g *GameStore) Teams(ctx context.Context, active bool) ([]domain.Team, error) {
	return readTeams(ctx, g.queries, active)
}

func readTeams(ctx context.Context, q *sqlc.Queries, active bool) ([]domain.Team, error) {
	const location = "postgres.readTeams"
	rows, err := q.ListGameTeams(ctx, active)
	if err != nil {
		return nil, fmt.Errorf("%s - list game teams: %w", location, err)
	}
	result := make([]domain.Team, 0, len(rows))
	for _, r := range rows {
		result = append(result, domain.Team{ID: r.ID, Name: r.Name, IP: r.Ip, Token: r.Token, Highlighted: r.Highlighted, Active: r.Active, LogoPath: r.LogoPath})
	}
	return result, nil
}

func (g *GameStore) Tasks(ctx context.Context, active bool) ([]domain.Task, error) {
	return readTasks(ctx, g.queries, active)
}

func readTasks(ctx context.Context, q *sqlc.Queries, active bool) ([]domain.Task, error) {
	const location = "postgres.readTasks"
	rows, err := q.ListGameTasks(ctx, active)
	if err != nil {
		return nil, fmt.Errorf("%s - list game tasks: %w", location, err)
	}
	result := make([]domain.Task, 0, len(rows))
	for _, r := range rows {
		result = append(result, domain.Task{ID: r.ID, Name: r.Name, Checker: r.Checker, EnvPath: r.EnvPath, Gets: r.Gets, Puts: r.Puts, Places: r.Places, CheckerTimeout: r.CheckerTimeout, CheckerType: r.CheckerType, GetPeriod: r.GetPeriod, DefaultScore: r.DefaultScore, Active: r.Active})
	}
	return result, nil
}

func (g *GameStore) Cells(ctx context.Context) ([]domain.Cell, error) {
	return readCells(ctx, g.queries)
}

func readCells(ctx context.Context, q *sqlc.Queries) ([]domain.Cell, error) {
	const location = "postgres.readCells"
	rows, err := q.ListGameCells(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - list game cells: %w", location, err)
	}
	result := make([]domain.Cell, 0, len(rows))
	for _, r := range rows {
		result = append(result, domain.Cell{TeamID: r.TeamID, TaskID: r.TaskID, Status: r.Status, Stolen: r.Stolen, Lost: r.Lost, Score: r.Score, Checks: r.Checks, ChecksPassed: r.ChecksPassed, PublicMessage: r.PublicMessage, PrivateMessage: r.PrivateMessage, Command: r.Command})
	}
	return result, nil
}

func (g *GameStore) Board(ctx context.Context) (domain.BoardData, error) {
	const location = "postgres.GameStore.Board"
	data := domain.BoardData{}
	tx, err := g.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return data, fmt.Errorf("%s - begin board read transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	q := sqlc.New(tx)
	if data.Lifecycle, err = readLifecycle(ctx, q, true); err != nil {
		return data, fmt.Errorf("%s - read lifecycle: %w", location, err)
	}
	if data.Teams, err = readTeams(ctx, q, true); err != nil {
		return data, fmt.Errorf("%s - read active teams: %w", location, err)
	}
	if data.Tasks, err = readTasks(ctx, q, true); err != nil {
		return data, fmt.Errorf("%s - read active tasks: %w", location, err)
	}
	if data.Cells, err = readCells(ctx, q); err != nil {
		return data, fmt.Errorf("%s - read team task cells: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return data, fmt.Errorf("%s - commit board read transaction: %w", location, err)
	}
	return data, nil
}

func (g *GameStore) History(ctx context.Context, teamID uuid.UUID, taskID *uuid.UUID) ([]domain.Cell, error) {
	const location = "postgres.GameStore.History"
	rows, err := g.queries.ReadGameHistory(ctx, sqlc.ReadGameHistoryParams{TeamID: teamID, TaskID: taskID})
	if err != nil {
		return nil, fmt.Errorf("%s - read game history: %w", location, err)
	}
	result := make([]domain.Cell, 0, len(rows))
	for _, r := range rows {
		if r.TeamID == nil || r.TaskID == nil {
			continue
		}
		result = append(result, domain.Cell{ID: r.ID, CreatedAt: r.Ts.Time, TeamID: *r.TeamID, TaskID: *r.TaskID, Status: r.Status, Stolen: r.Stolen, Lost: r.Lost, Score: r.Score, Checks: r.Checks, ChecksPassed: r.ChecksPassed, PublicMessage: r.PublicMessage, PrivateMessage: r.PrivateMessage, Command: r.Command, Round: r.Round, Timestamp: strconv.FormatInt(r.Ts.Time.UnixMilli(), 10) + "-" + r.ID.String()})
	}
	return result, nil
}

// Public history contains completed round snapshots, never partial checker
// results or private diagnostic commands from the organizer's event log.
func (g *GameStore) PublicHistory(ctx context.Context, teamID uuid.UUID) ([]domain.PublicCell, error) {
	const location = "postgres.GameStore.PublicHistory"
	rows, err := g.queries.ReadPublicGameHistory(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("%s - read public game history: %w", location, err)
	}
	result := make([]domain.PublicCell, 0, len(rows))
	for _, row := range rows {
		var cell domain.PublicCell
		if err := json.Unmarshal(row.Cell, &cell); err != nil {
			return nil, fmt.Errorf("%s - decode public round history: %w", location, err)
		}
		cell.Round = row.Round
		cell.Timestamp = strconv.FormatInt(int64(row.RoundStart*1000), 10) + "-" + strconv.Itoa(int(row.Round))
		result = append(result, cell)
	}
	return result, nil
}

func rollbackTransaction(ctx context.Context, tx pgx.Tx) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), transactionCleanupTimeout)
	defer cancel()
	_ = tx.Rollback(cleanupCtx)
}

func gameDBError(err error) error {
	const location = "postgres.gameDBError"
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s - map missing database row: %w", location, domain.ErrNotFound)
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		switch pgerr.Code {
		case "23505", "23503", "23514", "23502":
			return fmt.Errorf("%s - map database constraint violation: %w", location, domain.ErrConflict)
		}
	}
	return err
}

func (g *GameStore) transaction(ctx context.Context, apply func(*sqlc.Queries) error) error {
	const location = "postgres.GameStore.transaction"
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s - begin game mutation transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	q := sqlc.New(tx)
	if _, err = q.LockGameForSession(ctx); err != nil {
		return fmt.Errorf("%s - lock game before mutation: %w", location, gameDBError(err))
	}
	if err = apply(q); err != nil {
		return fmt.Errorf("%s - apply game mutation: %w", location, gameDBError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s - commit game mutation transaction: %w", location, gameDBError(err))
	}
	return nil
}

func (g *GameStore) CreateTeam(ctx context.Context, team domain.Team) (domain.Team, error) {
	const location = "postgres.GameStore.CreateTeam"
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		id, err := q.InsertGameTeam(ctx, sqlc.InsertGameTeamParams{Name: team.Name, Ip: team.IP, Token: team.Token, Highlighted: new(team.Highlighted), Active: new(team.Active), LogoPath: new(team.LogoPath)})
		if err != nil {
			return fmt.Errorf("%s - insert game team: %w", location, err)
		}
		team.ID = id
		if err := q.InsertTeamCells(ctx, id); err != nil {
			return fmt.Errorf("%s - initialize team task cells: %w", location, err)
		}
		return nil
	})
	return team, err
}

func (g *GameStore) UpdateTeam(ctx context.Context, team domain.Team) (domain.Team, error) {
	const location = "postgres.GameStore.UpdateTeam"
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		n, err := q.ReplaceGameTeam(ctx, sqlc.ReplaceGameTeamParams{ID: team.ID, Name: team.Name, Ip: team.IP, Token: team.Token, Highlighted: new(team.Highlighted), Active: new(team.Active), LogoPath: new(team.LogoPath)})
		if err != nil {
			return fmt.Errorf("%s - replace game team: %w", location, err)
		}
		if n == 0 {
			return fmt.Errorf("%s - replace game team: %w", location, domain.ErrNotFound)
		}
		return nil
	})
	return team, err
}

func (g *GameStore) DeactivateTeam(ctx context.Context, id uuid.UUID) error {
	const location = "postgres.GameStore.DeactivateTeam"
	return g.transaction(ctx, func(q *sqlc.Queries) error {
		n, err := q.DisableGameTeam(ctx, id)
		if err != nil {
			return fmt.Errorf("%s - deactivate game team: %w", location, err)
		}
		if n == 0 {
			return fmt.Errorf("%s - deactivate game team: %w", location, domain.ErrNotFound)
		}
		return nil
	})
}

func (g *GameStore) CreateTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	const location = "postgres.GameStore.CreateTask"
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		if err := validateExtraCheckTimeout(ctx, q, task); err != nil {
			return err
		}
		id, err := q.InsertGameTask(ctx, sqlc.InsertGameTaskParams{Name: new(task.Name), Checker: new(task.Checker), EnvPath: new(task.EnvPath), Gets: new(task.Gets), Puts: new(task.Puts), Places: new(task.Places), CheckerTimeout: new(task.CheckerTimeout), CheckerType: new(task.CheckerType), GetPeriod: new(task.GetPeriod), DefaultScore: new(task.DefaultScore), Active: new(task.Active)})
		if err != nil {
			return fmt.Errorf("%s - insert game task: %w", location, err)
		}
		task.ID = id
		if err := q.InsertTaskCells(ctx, id); err != nil {
			return fmt.Errorf("%s - initialize task cells: %w", location, err)
		}
		return nil
	})
	return task, err
}

func (g *GameStore) UpdateTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	const location = "postgres.GameStore.UpdateTask"
	err := g.transaction(ctx, func(q *sqlc.Queries) error {
		if err := validateExtraCheckTimeout(ctx, q, task); err != nil {
			return err
		}
		n, err := q.ReplaceGameTask(ctx, sqlc.ReplaceGameTaskParams{ID: task.ID, Name: new(task.Name), Checker: new(task.Checker), EnvPath: new(task.EnvPath), Gets: new(task.Gets), Puts: new(task.Puts), Places: new(task.Places), CheckerTimeout: new(task.CheckerTimeout), CheckerType: new(task.CheckerType), GetPeriod: new(task.GetPeriod), DefaultScore: new(task.DefaultScore), Active: new(task.Active)})
		if err != nil {
			return fmt.Errorf("%s - replace game task: %w", location, err)
		}
		if n == 0 {
			return fmt.Errorf("%s - replace game task: %w", location, domain.ErrNotFound)
		}
		return nil
	})
	return task, err
}

func (g *GameStore) DeactivateTask(ctx context.Context, id uuid.UUID) error {
	const location = "postgres.GameStore.DeactivateTask"
	return g.transaction(ctx, func(q *sqlc.Queries) error {
		n, err := q.DisableGameTask(ctx, id)
		if err != nil {
			return fmt.Errorf("%s - deactivate game task: %w", location, err)
		}
		if n == 0 {
			return fmt.Errorf("%s - deactivate game task: %w", location, domain.ErrNotFound)
		}
		return nil
	})
}

func (g *GameStore) ChangeState(ctx context.Context, action string, generation *int64, now time.Time) error {
	const location = "postgres.GameStore.ChangeState"
	return g.transaction(ctx, func(q *sqlc.Queries) error {
		life, err := readLifecycle(ctx, q, false)
		if err != nil {
			return fmt.Errorf("%s - load current game state: %w", location, err)
		}
		if generation != nil && *generation != life.Session.Generation {
			return fmt.Errorf("%s - validate session generation: %w", location, &domain.StateError{Message: "Game session has changed"})
		}
		switch action {
		case "start", "start_practice", "start_final":
			if err := startGameSession(ctx, q, life, action, now); err != nil {
				return fmt.Errorf("%s - start game: %w", location, err)
			}
			return nil
		case "pause", "resume", "finish":
			if err := changeActiveGameSession(ctx, q, life, action, now); err != nil {
				return fmt.Errorf("%s - change active game state: %w", location, err)
			}
			return nil
		default:
			return fmt.Errorf("%s - validate game action: %w", location, domain.ErrNotFound)
		}
	})
}

func startGameSession(
	ctx context.Context,
	q *sqlc.Queries,
	life domain.Lifecycle,
	action string,
	now time.Time,
) error {
	const location = "postgres.startGameSession"
	if !life.Runtime(now).CanStart {
		return fmt.Errorf("%s - validate game start state: %w", location, &domain.StateError{Message: "Game cannot be started in its current state"})
	}
	early := now.Before(life.Config.StartTime)
	if action == "start_practice" && !early {
		return fmt.Errorf("%s - validate rehearsal start time: %w", location, &domain.StateError{Message: "The scheduled start has arrived; rehearsal cannot start"})
	}
	params := sqlc.StartGameSessionParams{}
	if action == "start_final" {
		params.FinalStart = gameStamp(now)
	} else if early {
		params.PracticeStart = gameStamp(now)
	}
	if err := q.StartGameSession(ctx, params); err != nil {
		return fmt.Errorf("%s - start game session: %w", location, err)
	}
	if err := q.SetRunning(ctx, new(true)); err != nil {
		return fmt.Errorf("%s - mark game as running: %w", location, err)
	}
	return nil
}

func changeActiveGameSession(
	ctx context.Context,
	q *sqlc.Queries,
	life domain.Lifecycle,
	action string,
	now time.Time,
) error {
	const location = "postgres.changeActiveGameSession"
	if life.Session.ResetPending {
		return fmt.Errorf("%s - validate reset state: %w", location, &domain.StateError{Message: "Rehearsal cleanup is still in progress"})
	}
	if life.Finished() {
		if action == "finish" {
			return nil
		}
		return fmt.Errorf("%s - validate game state: %w", location, &domain.StateError{Message: "Game has already finished"})
	}
	if action == "resume" {
		if err := q.ResumeExtraChecks(ctx, gameStamp(now)); err != nil {
			return fmt.Errorf("%s - resume delayed checks: %w", location, err)
		}
		if _, err := q.ResumeSession(ctx, gameStamp(now)); err != nil {
			return fmt.Errorf("%s - resume game session: %w", location, err)
		}
		return nil
	}
	if action == "finish" {
		if err := finishGameSession(ctx, q, life); err != nil {
			return fmt.Errorf("%s - finish game: %w", location, err)
		}
	}
	if _, err := q.PauseSession(ctx, gameStamp(now)); err != nil {
		return fmt.Errorf("%s - pause game session: %w", location, err)
	}
	return nil
}

func finishGameSession(ctx context.Context, q *sqlc.Queries, life domain.Lifecycle) error {
	const location = "postgres.finishGameSession"
	if life.Config.RealRound < 1 && life.Session.PracticeStart == nil && life.Session.FinalStart == nil {
		return fmt.Errorf("%s - validate game finish state: %w", location, &domain.StateError{Message: "Game has not started"})
	}
	if err := q.SetRunning(ctx, new(false)); err != nil {
		return fmt.Errorf("%s - stop game: %w", location, err)
	}
	if err := q.CancelUnsentJobs(ctx); err != nil {
		return fmt.Errorf("%s - cancel unsent checker jobs: %w", location, err)
	}
	return nil
}

// SetRoundLimit changes only the configured boundary; it never resumes or resets
// a session. The scheduler applies the new boundary on its next iteration.
func (g *GameStore) SetRoundLimit(ctx context.Context, rounds *int32) error {
	const location = "postgres.GameStore.SetRoundLimit"
	if rounds != nil && *rounds < 1 {
		return fmt.Errorf("%s - validate round limit: %w", location, &domain.StateError{Message: "Round limit must be positive"})
	}
	rows, err := g.queries.SetGameRoundLimit(ctx, rounds)
	if err != nil {
		return fmt.Errorf("%s - update game round limit: %w", location, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s - update game round limit: %w", location, domain.ErrNotFound)
	}
	return nil
}

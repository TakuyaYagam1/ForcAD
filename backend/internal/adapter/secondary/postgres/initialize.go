package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

// Initialize creates a game once, atomically. An existing game is never reset or
// overwritten by a container restart or a repeated init command.
func (g *GameStore) Initialize(ctx context.Context, document config.Document) (bool, error) {
	const location = "postgres.GameStore.Initialize"

	if err := document.Validate(); err != nil {
		return false, fmt.Errorf("%s - validate game configuration: %w", location, err)
	}
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - begin initialization transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	q := sqlc.New(tx)
	if err := q.LockInitialization(ctx); err != nil {
		return false, fmt.Errorf("%s - lock game initialization: %w", location, err)
	}
	exists, err := q.GameIsInitialized(ctx)
	if err != nil {
		return false, fmt.Errorf("%s - check whether game is initialized: %w", location, err)
	}
	if exists {
		return false, nil
	}
	start, err := document.Game.StartAt()
	if err != nil {
		return false, fmt.Errorf("%s - resolve game start time: %w", location, err)
	}
	if err := initializeGameConfig(ctx, q, document.Game, start); err != nil {
		return false, fmt.Errorf("%s - initialize game configuration: %w", location, err)
	}
	if err := initializeTeams(ctx, q, document.Teams); err != nil {
		return false, fmt.Errorf("%s - initialize teams: %w", location, err)
	}
	if err := initializeTasks(ctx, q, document.Tasks); err != nil {
		return false, fmt.Errorf("%s - initialize tasks: %w", location, err)
	}
	if err := q.InitializeGameCells(ctx); err != nil {
		return false, fmt.Errorf("%s - initialize team task cells: %w", location, err)
	}
	if err := saveInitialSnapshot(ctx, q); err != nil {
		return false, fmt.Errorf("%s - save initial snapshot: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("%s - commit initialization transaction: %w", location, err)
	}
	return true, nil
}

func initializeGameConfig(ctx context.Context, q *sqlc.Queries, game config.Game, start time.Time) error {
	const location = "postgres.initializeGameConfig"
	flagLifetime, err := checkedInt32(game.FlagLifetime)
	if err != nil {
		return fmt.Errorf("%s - convert flag lifetime: %w", location, err)
	}
	roundTime, err := checkedInt32(game.RoundTime)
	if err != nil {
		return fmt.Errorf("%s - convert round time: %w", location, err)
	}
	rounds, err := checkedInt32Pointer(game.Rounds)
	if err != nil {
		return fmt.Errorf("%s - convert round limit: %w", location, err)
	}
	err = q.InitializeGame(ctx, sqlc.InitializeGameParams{
		FlagLifetime: &flagLifetime, GameHardness: new(game.GameHardness),
		Inflation: new(game.Inflation), VolgaAttacksMode: new(game.VolgaAttacksMode),
		RoundTime: &roundTime, Rounds: rounds, Mode: new(game.Mode),
		Timezone: new(game.Timezone), StartTime: gameStamp(start),
	})
	if err != nil {
		return fmt.Errorf("%s - insert game configuration: %w", location, err)
	}
	return nil
}

func initializeTeams(ctx context.Context, q *sqlc.Queries, teams []config.Team) error {
	const location = "postgres.initializeTeams"
	for _, team := range teams {
		token := team.Token
		if token == "" {
			var value [8]byte
			if _, err := rand.Read(value[:]); err != nil {
				return fmt.Errorf("%s - generate team token: %w", location, err)
			}
			token = hex.EncodeToString(value[:])
		}
		_, err := q.InsertGameTeam(ctx, sqlc.InsertGameTeamParams{
			Name: team.Name, Ip: team.IP, Token: token,
			Highlighted: new(team.Highlighted), Active: new(true), LogoPath: new(team.LogoPath),
		})
		if err != nil {
			return fmt.Errorf("%s - insert game team: %w", location, err)
		}
	}
	return nil
}

func initializeTasks(ctx context.Context, q *sqlc.Queries, tasks []config.Task) error {
	const location = "postgres.initializeTasks"
	for _, task := range tasks {
		params, err := initialTaskParams(task)
		if err != nil {
			return fmt.Errorf("%s - prepare game task parameters: %w", location, err)
		}
		if _, err := q.InsertGameTask(ctx, params); err != nil {
			return fmt.Errorf("%s - insert game task: %w", location, err)
		}
	}
	return nil
}

func initialTaskParams(task config.Task) (sqlc.InsertGameTaskParams, error) {
	const location = "postgres.initialTaskParams"
	type convertedTaskLimits struct {
		gets, puts, places, timeout, getPeriod, defaultScore int32
	}
	var converted convertedTaskLimits
	values := []struct {
		name   string
		value  *int
		output *int32
	}{
		{"gets", task.Gets, &converted.gets},
		{"puts", task.Puts, &converted.puts},
		{"places", task.Places, &converted.places},
		{"checker timeout", task.CheckerTimeout, &converted.timeout},
		{"get period", task.GetPeriod, &converted.getPeriod},
		{"default score", task.DefaultScore, &converted.defaultScore},
	}
	for _, item := range values {
		if item.value == nil {
			return sqlc.InsertGameTaskParams{}, fmt.Errorf("%s - require %s: %w", location, item.name, domain.ErrInvalidGameConfig)
		}
		value, err := checkedInt32(*item.value)
		if err != nil {
			return sqlc.InsertGameTaskParams{}, fmt.Errorf("%s - convert %s: %w", location, item.name, err)
		}
		*item.output = value
	}
	return sqlc.InsertGameTaskParams{
		Name: new(task.Name), Checker: new(task.Checker), EnvPath: task.EnvPath,
		Gets: &converted.gets, Puts: &converted.puts, Places: &converted.places,
		CheckerTimeout: &converted.timeout, CheckerType: new(task.CheckerType),
		GetPeriod: &converted.getPeriod, DefaultScore: &converted.defaultScore, Active: new(true),
	}, nil
}

func saveInitialSnapshot(ctx context.Context, q *sqlc.Queries) error {
	const location = "postgres.saveInitialSnapshot"
	cells, err := q.ListGameCells(ctx)
	if err != nil {
		return fmt.Errorf("%s - load initialized team task cells: %w", location, err)
	}
	state := domain.GameState{TeamTasks: make([]domain.PublicCell, 0, len(cells))}
	for _, cell := range cells {
		state.TeamTasks = append(state.TeamTasks, domain.PublicCell{
			TeamID: cell.TeamID, TaskID: cell.TaskID, Status: cell.Status, Score: cell.Score,
		})
	}
	snapshot, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("%s - encode initial game snapshot: %w", location, err)
	}
	if _, err := q.SaveGameSnapshot(ctx, sqlc.SaveGameSnapshotParams{
		SnapshotRound: new(int32(0)), Snapshot: snapshot,
	}); err != nil {
		return fmt.Errorf("%s - save initial game snapshot: %w", location, err)
	}
	return nil
}

var errInt32OutOfRange = errors.New("value exceeds the database integer range")

func checkedInt32(value int) (int32, error) {
	const location = "postgres.checkedInt32"
	const minInt32 = -1 << 31
	const maxInt32 = 1<<31 - 1
	if value < minInt32 || value > maxInt32 {
		return 0, fmt.Errorf("%s - validate integer range: %w", location, errInt32OutOfRange)
	}
	return int32(value), nil
}

func checkedInt32Pointer(value *int) (*int32, error) {
	const location = "postgres.checkedInt32Pointer"
	if value == nil {
		return nil, nil
	}
	converted, err := checkedInt32(*value)
	if err != nil {
		return nil, fmt.Errorf("%s - convert integer pointer: %w", location, err)
	}
	return &converted, nil
}

// ResetGameData is used only by the explicitly confirmed maintenance command
// after application writers have stopped. It keeps the schema and migration
// ledger. Foreign keys from unknown tables cause rollback instead of cascading.
func (g *GameStore) ResetGameData(ctx context.Context) error {
	const location = "postgres.GameStore.ResetGameData"

	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s - begin reset transaction: %w", location, err)
	}
	defer rollbackTransaction(ctx, tx)
	q := sqlc.New(tx)
	if err := q.LockInitialization(ctx); err != nil {
		return fmt.Errorf("%s - lock game initialization for reset: %w", location, err)
	}
	if err := q.ResetGameTables(ctx); err != nil {
		return fmt.Errorf("%s - clear game tables: %w", location, err)
	}
	if err := q.InitializeEmptySession(ctx); err != nil {
		return fmt.Errorf("%s - initialize empty game session: %w", location, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s - commit reset transaction: %w", location, err)
	}
	return nil
}

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func gameDocument(t *testing.T) config.Document {
	t.Helper()
	document, err := config.DecodeDocument(strings.NewReader(`game:
  mode: classic
  round_time: 60
  rounds: 3
  start_time: 2050-01-01 00:00:00
  flag_lifetime: 3
tasks:
  - name: example
    checker: example/checker.py
teams:
  - name: first
    ip: 127.0.0.1
    token: "0000000000000001"
  - name: second
    ip: 127.0.0.2
    token: "0000000000000002"
`))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestInitializationPreservesExistingGame(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	store := postgres.NewGameStore(pool)
	document := gameDocument(t)
	created, err := store.Initialize(ctx, document)
	if err != nil || !created {
		t.Fatalf("initialize: %v %v", created, err)
	}
	board, err := store.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Teams) != 2 || len(board.Tasks) != 1 || len(board.Cells) != 2 {
		t.Fatalf("initial board: %+v", board)
	}
	var snapshot domain.GameState
	if err := json.Unmarshal(board.Lifecycle.Session.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Round != 0 || len(snapshot.TeamTasks) != 2 || snapshot.TeamTasks[0].Score != 2500 {
		t.Fatalf("initial snapshot: %+v", snapshot)
	}
	team := board.Teams[0]
	team.Name = "edited"
	if _, err := store.UpdateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	created, err = store.Initialize(ctx, document)
	if err != nil || created {
		t.Fatalf("repeated initialization: %v %v", created, err)
	}
	teams, err := store.Teams(ctx, false)
	if err != nil || teams[0].Name != "edited" || teams[0].Token != "0000000000000001" {
		t.Fatalf("existing data overwritten: %+v %v", teams, err)
	}
}

func TestGameActionsPersistPauseAndRejectStaleGeneration(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	if err := store.ChangeState(ctx, "start_final", new(int64(0)), now); err != nil {
		t.Fatal(err)
	}
	life, err := store.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !life.Config.GameRunning || life.Session.FinalStart == nil || life.Session.PracticeStart != nil || life.Session.Generation != 1 {
		t.Fatalf("start final: %+v", life)
	}
	if err := store.ChangeState(ctx, "pause", new(int64(0)), now); err == nil {
		t.Fatal("accepted stale generation")
	}
	if err := store.ChangeState(ctx, "pause", new(int64(1)), now); err != nil {
		t.Fatal(err)
	}
	if err := store.ChangeState(ctx, "resume", new(int64(1)), now.Add(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	life, err = store.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if life.Session.PausedAt != nil || life.Session.PausedSeconds != 12 {
		t.Fatalf("pause accounting: %+v", life.Session)
	}
	if err := store.ChangeState(ctx, "finish", nil, now.Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.ChangeState(ctx, "resume", nil, now.Add(16*time.Second)); err == nil {
		t.Fatal("resumed finished final")
	}
	if err := store.ChangeState(ctx, "start_final", nil, now.Add(17*time.Second)); err == nil {
		t.Fatal("restarted finished final")
	}
	if err := store.ChangeState(ctx, "unknown", nil, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown action: %v", err)
	}
}

func TestPublicHistoryIncludesOnlyCompletedRoundSnapshots(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, store)
	var extraTaskID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO Tasks (name) VALUES ('history-extra') RETURNING id`).Scan(&extraTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasksLog(team_id,task_id,round,status,public_message,private_message,command)
		VALUES($1,$2,2,103,'unfinished','private diagnostic','private command')`, teams[0], tasks[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE TeamTasksLog SET ts = now() - interval '1 minute' WHERE round = 2 AND team_id = $1`, teams[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasksLog(team_id,task_id,round,status,public_message,private_message,command)
		VALUES($1,$2,1,101,'completed','private completed','private command')`, teams[0], extraTaskID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(domain.RoundSnapshot{Round: 1, RoundStart: 1700000000,
		TeamTasks: []domain.PublicCell{{TeamID: teams[0], TaskID: tasks[0], Status: 101, Score: 2500, Checks: 1, ChecksPassed: 1, Message: "UP"}, {TeamID: teams[1], TaskID: tasks[0], Status: 104}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO GameRoundSnapshots(round,round_start,snapshot) VALUES(1,1700000000,$1)`, snapshot); err != nil {
		t.Fatal(err)
	}
	public, err := store.PublicHistory(ctx, teams[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || public[0].Round != 1 || public[0].TeamID != teams[0] || public[0].Status != 101 || public[0].Message != "UP" {
		t.Fatalf("public history: %+v", public)
	}
	private, err := store.History(ctx, teams[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(private) != 2 || private[0].PrivateMessage != "private completed" || private[0].Round != 1 ||
		private[1].PrivateMessage != "private diagnostic" || private[1].Round != 2 {
		t.Fatalf("organizer history: %+v", private)
	}
	filtered, err := store.History(ctx, teams[0], &tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Round != 2 {
		t.Fatalf("task-filtered organizer history: %+v", filtered)
	}
}

func TestRoundLimitUpdatePreservesFinishedGame(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	if err := game.ChangeState(ctx, "start_final", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE GameConfig SET real_round=7`); err != nil {
		t.Fatal(err)
	}
	if err := game.ChangeState(ctx, "finish", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, rounds := range []*int32{new(int32(300)), nil} {
		if err := game.SetRoundLimit(ctx, rounds); err != nil {
			t.Fatal(err)
		}
		after, err := game.Lifecycle(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !after.Finished() || after.Config.RealRound != before.Config.RealRound || after.Session.Generation != before.Session.Generation {
			t.Fatalf("round limit changed terminal lifecycle: %+v", after)
		}
		if (rounds == nil) != (after.Config.Rounds == nil) || rounds != nil && *rounds != *after.Config.Rounds {
			t.Fatalf("round limit not updated: %v", after.Config.Rounds)
		}
	}
}

func TestConfirmedResetPreservesSchemaAndMigrationLedger(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	store := postgres.NewGameStore(pool)
	document := gameDocument(t)
	if _, err := store.Initialize(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE unrelated (value TEXT); INSERT INTO unrelated VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	var migrationsBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&migrationsBefore); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.ResetGameData(ctx); err != nil {
			t.Fatal(err)
		}
		var games, teams, sessions, migrations, unrelated int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM GameConfig), (SELECT count(*) FROM Teams), (SELECT count(*) FROM GameSession), (SELECT count(*) FROM goose_db_version), (SELECT count(*) FROM unrelated)`).Scan(&games, &teams, &sessions, &migrations, &unrelated); err != nil {
			t.Fatal(err)
		}
		if games != 0 || teams != 0 || sessions != 1 || migrations != migrationsBefore || unrelated != 1 {
			t.Fatalf("reset affected schema or unrelated data: %d %d %d %d %d", games, teams, sessions, migrations, unrelated)
		}
	}
	if created, err := store.Initialize(ctx, document); err != nil || !created {
		t.Fatalf("reinitialize after reset: created=%v err=%v", created, err)
	}
	teams, err := store.Teams(ctx, false)
	if err != nil || len(teams) != len(document.Teams) || teams[0].Token != document.Teams[0].Token {
		t.Fatalf("configured teams/tokens not restored: %v %v", teams, err)
	}
}

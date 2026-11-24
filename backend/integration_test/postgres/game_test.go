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
	snapshot, err := json.Marshal(domain.RoundSnapshot{
		Round:      1,
		RoundStart: 1700000000,
		TeamTasks: []domain.PublicCell{{
			TeamID:       teams[0],
			TaskID:       tasks[0],
			Status:       101,
			Score:        2500,
			Checks:       1,
			ChecksPassed: 1,
			Message:      "UP",
		}, {
			TeamID: teams[1],
			TaskID: tasks[0],
			Status: 104,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO GameRoundSnapshots(round,round_start,snapshot) VALUES(1,1700000000,$1)`, snapshot); err != nil {
		t.Fatal(err)
	}
	public, err := store.PublicHistory(ctx, teams[0], nil, nil)
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

func TestPublicHistoryPagesMatchingCompletedRounds(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	store := postgres.NewGameStore(pool)
	document := gameDocument(t)
	otherTask := document.Tasks[0]
	otherTask.Name = "other"
	document.Tasks = append(document.Tasks, otherTask)
	if _, err := store.Initialize(ctx, document); err != nil {
		t.Fatal(err)
	}

	lookupID := func(query string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := pool.QueryRow(ctx, query).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	teamID := lookupID(`SELECT id FROM Teams WHERE name = 'first'`)
	otherTeamID := lookupID(`SELECT id FROM Teams WHERE name = 'second'`)
	taskID := lookupID(`SELECT id FROM Tasks WHERE name = 'example'`)
	otherTaskID := lookupID(`SELECT id FROM Tasks WHERE name = 'other'`)

	if _, err := pool.Exec(ctx, `
		INSERT INTO GameRoundSnapshots (round, round_start, snapshot)
		SELECT round_number, start_epoch::DOUBLE PRECISION,
		       jsonb_build_object(
		           'round', round_number,
		           'round_start', start_epoch,
		           'team_tasks',
		           jsonb_build_array(
		               jsonb_build_object(
		                   'team_id', $1::UUID, 'task_id', $2::UUID,
		                   'status', 101, 'score', 2500
		               ),
		               jsonb_build_object(
		                   'team_id', $3::UUID, 'task_id', $4::UUID,
		                   'status', 102, 'score', 2400
		               )
		           ) || CASE WHEN round_number % 3 = 0 THEN
		               jsonb_build_array(jsonb_build_object(
		                   'team_id', $5::UUID, 'task_id', $6::UUID,
		                   'status', 101, 'score', 2500
		               ))
		           ELSE '[]'::JSONB END
		       )
		FROM (
		    SELECT round_number, 1700000000 + round_number * 60 AS start_epoch
		    FROM generate_series(1, 420) AS rounds(round_number)
		) AS snapshot_data`,
		teamID, otherTaskID, otherTeamID, taskID, teamID, taskID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO DispatchRuns (schedule_id, round, is_round, completed, round_start)
		SELECT 'public-history', round_number, TRUE, round_number <= 420,
		       (1700000000 + round_number * 60)::DOUBLE PRECISION
		FROM generate_series(1, 421) AS rounds(round_number)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO TeamTasksLog (team_id, task_id, round, status, public_message, private_message, command)
		VALUES ($1, $2, 0, 103, 'unfinished', 'private diagnostic', 'private command'),
		       ($1, $2, 421, 103, 'unfinished', 'private diagnostic', 'private command')`,
		teamID, taskID,
	); err != nil {
		t.Fatal(err)
	}

	var capturedFlagID, lostFlagID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO Flags (team_id, task_id, round, flag, public_flag_data, private_flag_data)
		VALUES ($1, $2, 300, 'history-captured', '', '')
		RETURNING id`, otherTeamID, taskID).Scan(&capturedFlagID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO Flags (team_id, task_id, round, flag, public_flag_data, private_flag_data)
		VALUES ($1, $2, 300, 'history-lost', '', '')
		RETURNING id`, teamID, taskID).Scan(&lostFlagID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO StolenFlags (flag_id, attacker_id, submit_time, attacker_delta)
		VALUES ($1, $2, to_timestamp($3::DOUBLE PRECISION + 1), 7.5),
		       ($4, $5, to_timestamp($3::DOUBLE PRECISION + 2), 3.25)`,
		capturedFlagID, teamID, 1700000000+300*60, lostFlagID, otherTeamID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `UPDATE Teams SET active = FALSE WHERE id = $1`, teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE Tasks SET active = FALSE WHERE id = $1`, taskID); err != nil {
		t.Fatal(err)
	}

	defaultHistory, err := store.PublicHistory(ctx, teamID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defaultRounds := make(map[int32]struct{}, 50)
	targetRows := 0
	for _, row := range defaultHistory {
		if row.TeamID != teamID || row.Round < 371 || row.Round > 420 {
			t.Fatalf("default history escaped the latest 50 team snapshots: %+v", row)
		}
		defaultRounds[row.Round] = struct{}{}
		if row.TaskID == taskID {
			targetRows++
		}
	}
	if len(defaultRounds) != 50 || len(defaultHistory) != 67 || targetRows != 17 {
		t.Fatalf("default history: rounds=%d rows=%d matching service=%d", len(defaultRounds), len(defaultHistory), targetRows)
	}
	if defaultHistory[0].Round != 420 || defaultHistory[len(defaultHistory)-1].Round != 371 {
		t.Fatalf("default history not newest first: first=%d last=%d", defaultHistory[0].Round, defaultHistory[len(defaultHistory)-1].Round)
	}

	readServicePage := func(throughRound *int32) []domain.PublicCell {
		t.Helper()
		rows, err := store.PublicHistory(ctx, teamID, &taskID, throughRound)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	assertServicePage := func(
		name string,
		rows []domain.PublicCell,
		wantCount, wantFirst, wantLast int32,
	) {
		t.Helper()
		if len(rows) != int(wantCount) {
			t.Fatalf("%s page size = %d, want %d", name, len(rows), wantCount)
		}
		for i, row := range rows {
			if row.TeamID != teamID || row.TaskID != taskID || row.Round == 0 || row.Round > 420 {
				t.Fatalf("%s page exposed a non-snapshot/non-service row: %+v", name, row)
			}
			if i > 0 && rows[i-1].Round-row.Round != 3 {
				t.Fatalf("%s page rounds are not unique, descending sparse rounds: %d then %d",
					name, rows[i-1].Round, row.Round)
			}
		}
		if rows[0].Round != wantFirst || rows[len(rows)-1].Round != wantLast {
			t.Fatalf("%s page range = %d..%d, want %d..%d",
				name, rows[0].Round, rows[len(rows)-1].Round, wantFirst, wantLast)
		}
	}

	firstPage := readServicePage(nil)
	assertServicePage("first", firstPage, 50, 420, 273)
	cursor := firstPage[len(firstPage)-1].Round - 1
	secondPage := readServicePage(&cursor)
	assertServicePage("second", secondPage, 50, 270, 123)
	if firstPage[len(firstPage)-1].Round-secondPage[0].Round < 2 {
		t.Fatalf("cursor crossed fewer than two round numbers: %d then %d",
			firstPage[len(firstPage)-1].Round, secondPage[0].Round)
	}
	cursor = secondPage[len(secondPage)-1].Round - 1
	thirdPage := readServicePage(&cursor)
	assertServicePage("third", thirdPage, 40, 120, 3)
	cursor = thirdPage[len(thirdPage)-1].Round - 1
	if remaining := readServicePage(&cursor); len(remaining) != 0 {
		t.Fatalf("history remained below the final cursor: %+v", remaining)
	}

	inclusiveRound := int32(300)
	inclusivePage := readServicePage(&inclusiveRound)
	assertServicePage("inclusive", inclusivePage, 50, 300, 153)
	round300 := firstPage[(420-300)/3]
	if round300.Round != 300 || round300.RoundFlagsCaptured == nil || *round300.RoundFlagsCaptured != 1 ||
		round300.RoundFPGained == nil || *round300.RoundFPGained != 7.5 ||
		round300.RoundFlagsLost == nil || *round300.RoundFlagsLost != 1 ||
		round300.RoundOpponentFP == nil || *round300.RoundOpponentFP != 3.25 {
		t.Fatalf("round 300 FP projection changed: %+v", round300)
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

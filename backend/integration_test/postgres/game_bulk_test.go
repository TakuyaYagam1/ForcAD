package postgres_test

import (
	"errors"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestDeactivateTeamsIsAtomicAndIdempotent(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Teams(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, len(before))
	tokens := make(map[uuid.UUID]string, len(before))
	for i, team := range before {
		ids[i] = team.ID
		tokens[team.ID] = team.Token
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasksLog(round,team_id,task_id,status)
		SELECT 1,$1,id,101 FROM Tasks ORDER BY id LIMIT 1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	var cellsBefore, historyBefore int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM TeamTasks), (SELECT count(*) FROM TeamTasksLog)`).Scan(&cellsBefore, &historyBefore); err != nil {
		t.Fatal(err)
	}

	count, err := store.DeactivateTeams(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if count != int32(len(ids)) {
		t.Fatalf("deactivated = %d, want %d", count, len(ids))
	}
	count, err = store.DeactivateTeams(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("idempotent deactivation count = %d, want 0", count)
	}
	after, err := store.Teams(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, team := range after {
		if team.Active {
			t.Fatalf("team %s is still active", team.ID)
		}
		if team.Token != tokens[team.ID] {
			t.Fatalf("team %s token changed", team.ID)
		}
	}
	var cellsAfter, historyAfter int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM TeamTasks), (SELECT count(*) FROM TeamTasksLog)`).Scan(&cellsAfter, &historyAfter); err != nil {
		t.Fatal(err)
	}
	if cellsAfter != cellsBefore || historyAfter != historyBefore {
		t.Fatalf("team-task rows = (%d, %d), want (%d, %d)", cellsAfter, historyAfter, cellsBefore, historyBefore)
	}

	if _, err := pool.Exec(ctx, `UPDATE Teams SET active=TRUE`); err != nil {
		t.Fatal(err)
	}
	missing := uuid.MustParse("ffffffff-ffff-7fff-bfff-ffffffffffff")
	count, err = store.DeactivateTeams(ctx, []uuid.UUID{ids[0], missing})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing team error = %v, want ErrNotFound", err)
	}
	if count != 0 {
		t.Fatalf("failed deactivation count = %d, want 0", count)
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM Teams WHERE id=$1`, ids[0]).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("existing team was deactivated when another requested team was missing")
	}
}

func TestDeactivateTasksIsAtomicAndIdempotent(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Tasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, len(before))
	for i, task := range before {
		ids[i] = task.ID
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasksLog(round,team_id,task_id,status)
		SELECT 1,id,$1,101 FROM Teams ORDER BY id LIMIT 1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	var cellsBefore, historyBefore int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM TeamTasks), (SELECT count(*) FROM TeamTasksLog)`).Scan(&cellsBefore, &historyBefore); err != nil {
		t.Fatal(err)
	}

	count, err := store.DeactivateTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if count != int32(len(ids)) {
		t.Fatalf("deactivated = %d, want %d", count, len(ids))
	}
	count, err = store.DeactivateTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("idempotent deactivation count = %d, want 0", count)
	}
	after, err := store.Tasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range after {
		if task.Active {
			t.Fatalf("task %s is still active", task.ID)
		}
	}
	var cellsAfter, historyAfter int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM TeamTasks), (SELECT count(*) FROM TeamTasksLog)`).Scan(&cellsAfter, &historyAfter); err != nil {
		t.Fatal(err)
	}
	if cellsAfter != cellsBefore || historyAfter != historyBefore {
		t.Fatalf("team-task rows = (%d, %d), want (%d, %d)", cellsAfter, historyAfter, cellsBefore, historyBefore)
	}

	if _, err := pool.Exec(ctx, `UPDATE Tasks SET active=TRUE`); err != nil {
		t.Fatal(err)
	}
	missing := uuid.MustParse("ffffffff-ffff-7fff-bfff-ffffffffffff")
	count, err = store.DeactivateTasks(ctx, []uuid.UUID{ids[0], missing})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing task error = %v, want ErrNotFound", err)
	}
	if count != 0 {
		t.Fatalf("failed deactivation count = %d, want 0", count)
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM Tasks WHERE id=$1`, ids[0]).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("existing task was deactivated when another requested task was missing")
	}
}

func TestActivateTeamsIsAtomicAndPreservesRelatedData(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, err := store.Teams(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) < 2 {
		t.Fatalf("teams = %d, want at least 2", len(teams))
	}
	if _, err := pool.Exec(ctx, `UPDATE Teams SET kind=$1 WHERE id=$2`, domain.TeamKindNPC, teams[1].ID); err != nil {
		t.Fatal(err)
	}
	teams, err = store.Teams(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, len(teams))
	for i, team := range teams {
		ids[i] = team.ID
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasksLog(round,team_id,task_id,status)
		SELECT 1,$1,id,101 FROM Tasks ORDER BY id LIMIT 1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	var teamRowsBefore, cellsBefore, historyBefore int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM Teams), (SELECT count(*) FROM TeamTasks), (SELECT count(*) FROM TeamTasksLog)`).Scan(
		&teamRowsBefore,
		&cellsBefore,
		&historyBefore,
	); err != nil {
		t.Fatal(err)
	}
	if count, err := store.DeactivateTeams(ctx, ids); err != nil || count != int32(len(ids)) {
		t.Fatalf("deactivate fixture teams: count=%d err=%v", count, err)
	}

	count, err := store.ActivateTeams(ctx, []uuid.UUID{ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("activated = %d, want 1", count)
	}
	count, err = store.ActivateTeams(ctx, []uuid.UUID{ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("idempotent activation count = %d, want 0", count)
	}
	after, err := store.Teams(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(teams) {
		t.Fatalf("teams = %d, want %d", len(after), len(teams))
	}
	expectedPlayer := teams[0]
	expectedNPC := teams[1]
	expectedNPC.Active = false
	if after[0] != expectedPlayer {
		t.Fatalf("activated team = %+v, want %+v", after[0], expectedPlayer)
	}
	if after[1] != expectedNPC {
		t.Fatalf("inactive NPC = %+v, want %+v", after[1], expectedNPC)
	}
	var teamRowsAfter, inactiveNPCs, cellsAfter, historyAfter int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM Teams),
		(SELECT count(*) FROM Teams WHERE kind='npc' AND NOT active),
		(SELECT count(*) FROM TeamTasks),
		(SELECT count(*) FROM TeamTasksLog)`).Scan(
		&teamRowsAfter,
		&inactiveNPCs,
		&cellsAfter,
		&historyAfter,
	); err != nil {
		t.Fatal(err)
	}
	if teamRowsAfter != teamRowsBefore || inactiveNPCs != 1 {
		t.Fatalf("team rows = %d and inactive NPCs = %d, want %d and 1", teamRowsAfter, inactiveNPCs, teamRowsBefore)
	}
	if cellsAfter != cellsBefore || historyAfter != historyBefore {
		t.Fatalf("team-task rows = (%d, %d), want (%d, %d)", cellsAfter, historyAfter, cellsBefore, historyBefore)
	}

	if count, err := store.DeactivateTeams(ctx, []uuid.UUID{ids[0]}); err != nil || count != 1 {
		t.Fatalf("prepare atomic activation: count=%d err=%v", count, err)
	}
	missing := uuid.MustParse("ffffffff-ffff-7fff-bfff-ffffffffffff")
	count, err = store.ActivateTeams(ctx, []uuid.UUID{ids[0], missing})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing team error = %v, want ErrNotFound", err)
	}
	if count != 0 {
		t.Fatalf("failed activation count = %d, want 0", count)
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM Teams WHERE id=$1`, ids[0]).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("existing team was activated when another requested team was missing")
	}
}

func TestActivateTasksIsAtomicAndPreservesRelatedData(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.Tasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 {
		t.Fatal("tasks fixture is empty")
	}
	ids := make([]uuid.UUID, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasksLog(round,team_id,task_id,status)
		SELECT 1,id,$1,101 FROM Teams ORDER BY id LIMIT 1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	var taskRowsBefore, cellsBefore, historyBefore int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM Tasks), (SELECT count(*) FROM TeamTasks), (SELECT count(*) FROM TeamTasksLog)`).Scan(
		&taskRowsBefore,
		&cellsBefore,
		&historyBefore,
	); err != nil {
		t.Fatal(err)
	}
	if count, err := store.DeactivateTasks(ctx, ids); err != nil || count != int32(len(ids)) {
		t.Fatalf("deactivate fixture tasks: count=%d err=%v", count, err)
	}

	count, err := store.ActivateTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if count != int32(len(ids)) {
		t.Fatalf("activated = %d, want %d", count, len(ids))
	}
	count, err = store.ActivateTasks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("idempotent activation count = %d, want 0", count)
	}
	after, err := store.Tasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(tasks) {
		t.Fatalf("tasks = %d, want %d", len(after), len(tasks))
	}
	for i := range tasks {
		if after[i] != tasks[i] {
			t.Fatalf("activated task = %+v, want %+v", after[i], tasks[i])
		}
	}
	var taskRowsAfter, cellsAfter, historyAfter int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM Tasks), (SELECT count(*) FROM TeamTasks), (SELECT count(*) FROM TeamTasksLog)`).Scan(
		&taskRowsAfter,
		&cellsAfter,
		&historyAfter,
	); err != nil {
		t.Fatal(err)
	}
	if taskRowsAfter != taskRowsBefore {
		t.Fatalf("task rows = %d, want %d", taskRowsAfter, taskRowsBefore)
	}
	if cellsAfter != cellsBefore || historyAfter != historyBefore {
		t.Fatalf("team-task rows = (%d, %d), want (%d, %d)", cellsAfter, historyAfter, cellsBefore, historyBefore)
	}

	if count, err := store.DeactivateTasks(ctx, []uuid.UUID{ids[0]}); err != nil || count != 1 {
		t.Fatalf("prepare atomic activation: count=%d err=%v", count, err)
	}
	missing := uuid.MustParse("ffffffff-ffff-7fff-bfff-ffffffffffff")
	count, err = store.ActivateTasks(ctx, []uuid.UUID{ids[0], missing})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing task error = %v, want ErrNotFound", err)
	}
	if count != 0 {
		t.Fatalf("failed activation count = %d, want 0", count)
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM Tasks WHERE id=$1`, ids[0]).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("existing task was activated when another requested task was missing")
	}
}

package postgres_test

import (
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestCheckerStatsUsesScheduledWorkAndIsolatedOutcomes(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	store := postgres.NewGameStore(pool)
	document := gameDocument(t)
	document.Tasks[0].Puts = new(5)
	other := document.Tasks[0]
	other.Name = "other"
	document.Tasks = append(document.Tasks, other)
	if _, err := store.Initialize(ctx, document); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, store)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE GameConfig SET extra_checks = 2`)
	preview, err := store.CheckerStats(ctx, teams[0], 0)
	if err != nil || len(preview) != 2 || preview[0].Check.Maximum != 3 || preview[0].Put != (domain.CheckerActionCount{
		Maximum: 5,
	}) {
		t.Fatalf("configuration preview = %+v, err = %v", preview, err)
	}
	exec(`UPDATE GameSession SET final_start = now() - interval '1 hour'`)
	var runID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO DispatchRuns(schedule_id, round, is_round, round_start)
		VALUES ('classic_rounds', 4, true, 0) RETURNING id`).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO CheckerJobs(run_id, round, team_id, task_id, action, payload, finished_at) VALUES
		($1, 4, $2, $3, 'classic', '{"task":{"puts":5,"gets":3}}', now()),
		($1, 4, $2, $3, 'check_only', '{}', NULL),
		($1, 4, $2, $4, 'puts_only', '{"task":{"puts":2,"gets":99}}', now()),
		($1, 4, $2, $4, 'check_gets', '{"task":{"puts":99,"gets":1}}', NULL),
		($1, 4, $5, $3, 'classic', '{"task":{"puts":5,"gets":3}}', now()),
		($1, 3, $2, $3, 'classic', '{"task":{"puts":5,"gets":3}}', now())`, runID, teams[0], tasks[0], tasks[1], teams[1])
	exec(`INSERT INTO CheckerActionLogs(team_id,task_id,round,action,stage,started_at,finished_at,duration_ms,status) VALUES
		($1,$2,4,'check','classic',now(),now(),0,101),
		($1,$2,4,'put','classic',now(),now(),0,101),
		($1,$2,4,'put','classic',now(),now(),0,102),
		($1,$2,4,'put','classic',now(),now(),0,103),
		($1,$2,4,'put','classic',now(),now(),0,104),
		($1,$2,4,'put','classic',now(),now(),0,110),
		($1,$2,4,'get','classic',now(),now(),0,104),
		($1,$3,4,'put','puts_only',now(),now(),0,101),
		($4,$2,4,'put','classic',now(),now(),0,104),
		($1,$2,3,'put','classic',now(),now(),0,104),
		($1,$2,4,'put','classic',now()-interval '2 hours',now()-interval '2 hours',0,104)`, teams[0], tasks[0], tasks[1], teams[1])
	rows, err := store.CheckerStats(ctx, teams[0], 4)
	if err != nil || len(rows) != 2 {
		t.Fatalf("checker stats = %+v, err = %v", rows, err)
	}
	if rows[0].TaskID != tasks[0] || rows[0].Check != (domain.CheckerActionCount{
		Maximum:  2,
		Reported: 1,
	}) ||
		rows[0].Put != (domain.CheckerActionCount{
			Maximum:  5,
			Reported: 5,
			Failed:   4,
		}) ||
		rows[0].Get != (domain.CheckerActionCount{
			Maximum:  3,
			Reported: 1,
			Failed:   1,
			NoResult: 2,
		}) {
		t.Errorf("classic with extra CHECK = %+v", rows[0])
	}
	if rows[1].TaskID != tasks[1] || rows[1].Check != (domain.CheckerActionCount{
		Maximum: 1,
	}) ||
		rows[1].Put != (domain.CheckerActionCount{
			Maximum:  2,
			Reported: 1,
			NoResult: 1,
		}) ||
		rows[1].Get != (domain.CheckerActionCount{
			Maximum: 1,
		}) {
		t.Errorf("separate blitz schedules = %+v", rows[1])
	}
	unscheduled, err := store.CheckerStats(ctx, teams[0], 5)
	if err != nil || len(unscheduled) != 2 || unscheduled[0].Put != (domain.CheckerActionCount{}) {
		t.Fatalf("round without jobs = %+v, err = %v", unscheduled, err)
	}

	// A manual start after the scheduled start can leave both session times
	// unset. Logs from the rehearsal still exist, but its dispatch runs do not.
	exec(`UPDATE GameSession SET practice_start = NULL, final_start = NULL`)
	exec(`UPDATE GameConfig SET start_time = now() - interval '1 day'`)
	exec(`UPDATE DispatchRuns SET round_start = EXTRACT(EPOCH FROM now() - interval '1 hour')::BIGINT WHERE id = $1`, runID)
	manual, err := store.CheckerStats(ctx, teams[0], 4)
	if err != nil || len(manual) != 2 || manual[0].Put != rows[0].Put {
		t.Fatalf("manual start included rehearsal replies: %+v, err = %v", manual, err)
	}
}

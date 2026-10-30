package checker_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	checkerworker "github.com/TakuyaYagam1/ForcAD/internal/worker/checker"
)

type runnerFunc func(context.Context, processchecker.Request) (processchecker.Result, error)

func (run runnerFunc) Run(ctx context.Context, request processchecker.Request) (processchecker.Result, error) {
	return run(ctx, request)
}

func TestWorkerPersistsWorkflowAndAcknowledgesRedelivery(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	jobID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	var teamID, taskID uuid.UUID

	if _, err := pool.Exec(ctx, `
		INSERT INTO GameConfig (id, game_running, real_round, flag_lifetime, game_hardness,
			inflation, volga_attacks_mode, round_time, mode, timezone, start_time)
		VALUES (1, FALSE, 7, 3, 1.1, FALSE, FALSE, 60, 'classic', 'UTC', now())`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO Teams (name, ip, token, active) VALUES ('red', '192.0.2.5', '0123456789abcdef', TRUE)
		RETURNING id`).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO Tasks (name, checker, gets, puts, places, checker_timeout,
			checker_type, get_period, default_score, active)
		VALUES ('service', '/fixture/checker', 0, 1, 3, 1, 'hackerdom_pfr', 0, 100, TRUE)
		RETURNING id`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasks (team_id, task_id, status, score) VALUES ($1, $2, -1, 100)`, teamID, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO DispatchRuns (id, schedule_id, round, is_round, round_start, snapshot)
		VALUES ($1, 'worker-integration', 7, TRUE, EXTRACT(EPOCH FROM now()), '{}'::jsonb)`, runID); err != nil {
		t.Fatal(err)
	}

	team := domain.Team{ID: teamID, Name: "red", IP: "192.0.2.5", Token: "0123456789abcdef", Active: true}
	task := domain.Task{ID: taskID, Name: "service", Checker: "/fixture/checker", Puts: 1,
		Places: 3, CheckerTimeout: 1, CheckerType: "hackerdom_pfr", Active: true}
	payload, err := json.Marshal(domain.JobPayload{Team: team, Task: task, Workflow: string(domain.JobActionPutsOnly)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO CheckerJobs (id, run_id, round, payload, action, team_id, task_id, sent_at, deadline_at)
		VALUES ($1, $2, 7, $3::jsonb, 'puts_only', $4, $5, now(), now() + interval '60 seconds')`,
		jobID, runID, string(payload), teamID, taskID); err != nil {
		t.Fatal(err)
	}

	repo := postgres.New(pool)
	runner := runnerFunc(func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		if request.Action != processchecker.ActionPut {
			t.Errorf("runner action = %q, want PUT", request.Action)
		}
		return processchecker.Result{Action: request.Action, Status: processchecker.StatusUp,
			PublicMessage: "put-public", PrivateMessage: "put-private", Command: "put fixture"}, nil
	})
	worker := checkerworker.New(repo, repo, postgres.NewGameStore(pool), runner,
		checkerworker.Options{LeaseDuration: 30 * time.Second, RenewInterval: 10 * time.Second, WorkConcurrency: 2}, nil)
	if err := worker.Handle(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	if err := worker.Handle(ctx, jobID); err != nil {
		t.Fatalf("redelivery after durable completion: %v", err)
	}

	var finished bool
	var status, checks, passed int32
	var public, private, command string
	if err := pool.QueryRow(ctx, `
		SELECT job.finished_at IS NOT NULL, cell.status, cell.checks, cell.checks_passed,
			cell.public_message, cell.private_message, cell.command
		FROM CheckerJobs AS job
		JOIN TeamTasks AS cell ON cell.team_id = job.team_id AND cell.task_id = job.task_id
		WHERE job.id = $1`, jobID).Scan(&finished, &status, &checks, &passed, &public, &private, &command); err != nil {
		t.Fatal(err)
	}
	if !finished || status != int32(processchecker.StatusUp) || checks != 1 || passed != 1 || public != "OK" || private != "put-private" || command != "put fixture" {
		t.Fatalf("durable workflow result = finished %t, status %d, checks %d/%d, messages %q/%q, command %q",
			finished, status, passed, checks, public, private, command)
	}
	var flagCount, logCount int
	var publicData, privateData string
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), max(public_flag_data), max(private_flag_data) FROM Flags WHERE team_id = $1 AND task_id = $2 AND round = 7`, teamID, taskID).Scan(&flagCount, &publicData, &privateData); err != nil {
		t.Fatal(err)
	}
	if flagCount != 1 || publicData != "put-public" || privateData != "put-private" {
		t.Fatalf("durable PUT flags = %d, data %q/%q", flagCount, publicData, privateData)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM TeamTasksLog WHERE team_id = $1 AND task_id = $2 AND round = 7`, teamID, taskID).Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if logCount != 1 {
		t.Fatalf("durable accounting rows = %d, want exactly one", logCount)
	}
	var actionLogCount int
	var action, stage, message string
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), max(action), max(stage), max(public_message)
		FROM CheckerActionLogs WHERE team_id = $1 AND task_id = $2 AND round = 7`,
		teamID, taskID,
	).Scan(&actionLogCount, &action, &stage, &message); err != nil {
		t.Fatal(err)
	}
	if actionLogCount != 1 || action != "put" || stage != "puts_only" || message != "PUT completed" {
		t.Fatalf("durable public checker log = %d rows, action %q, stage %q, message %q", actionLogCount, action, stage, message)
	}
	var privateColumns int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'checkeractionlogs'
		  AND column_name IN ('private_message', 'command', 'flag', 'private_data')`).Scan(&privateColumns); err != nil {
		t.Fatal(err)
	}
	if privateColumns != 0 {
		t.Fatalf("public checker log table has %d private columns", privateColumns)
	}
}

func TestCheckerLogWriteFailureDoesNotCompleteTheJob(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	jobID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	var teamID, taskID uuid.UUID
	if _, err := pool.Exec(ctx, `
		INSERT INTO GameConfig (id, game_running, real_round, flag_lifetime, game_hardness,
			inflation, volga_attacks_mode, round_time, mode, timezone, start_time)
		VALUES (1, FALSE, 7, 3, 1.1, FALSE, FALSE, 60, 'classic', 'UTC', now())`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO Teams (name, ip, token, active) VALUES ('red', '192.0.2.5', '1123456789abcdef', TRUE)
		RETURNING id`).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO Tasks (name, checker, gets, puts, places, checker_timeout,
			checker_type, get_period, default_score, active)
		VALUES ('service', '/fixture/checker', 0, 0, 3, 1, 'hackerdom', 0, 100, TRUE)
		RETURNING id`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasks (team_id, task_id, status, score) VALUES ($1, $2, -1, 100)`, teamID, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO DispatchRuns (id, schedule_id, round, is_round, round_start, snapshot)
		VALUES ($1, 'checker-log-failure', 7, TRUE, EXTRACT(EPOCH FROM now()), '{}'::jsonb)`, runID); err != nil {
		t.Fatal(err)
	}
	team := domain.Team{ID: teamID, Name: "red", IP: "192.0.2.5", Token: "1123456789abcdef", Active: true}
	task := domain.Task{ID: taskID, Name: "service", Checker: "/fixture/checker", CheckerTimeout: 1, Places: 3, CheckerType: "hackerdom", Active: true}
	payload, err := json.Marshal(domain.JobPayload{Team: team, Task: task, Workflow: string(domain.JobActionCheckOnly)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO CheckerJobs (id, run_id, round, payload, action, team_id, task_id, sent_at, deadline_at)
		VALUES ($1, $2, 7, $3::jsonb, 'check_only', $4, $5, now(), now() + interval '60 seconds')`,
		jobID, runID, string(payload), teamID, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_checker_action_log() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'checker log rejected'; END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TRIGGER reject_checker_action_log BEFORE INSERT ON CheckerActionLogs
		FOR EACH ROW EXECUTE FUNCTION reject_checker_action_log()`); err != nil {
		t.Fatal(err)
	}

	repo := postgres.New(pool)
	runner := runnerFunc(func(_ context.Context, request processchecker.Request) (processchecker.Result, error) {
		return processchecker.Result{Action: request.Action, Status: processchecker.StatusUp, PublicMessage: "UP"}, nil
	})
	worker := checkerworker.New(repo, repo, postgres.NewGameStore(pool), runner,
		checkerworker.Options{LeaseDuration: 30 * time.Second, RenewInterval: 10 * time.Second}, nil)
	if err := worker.Handle(ctx, jobID); err == nil {
		t.Fatal("worker acknowledged a job after its public log write failed")
	}
	var finished bool
	var checks, logs, snapshots int
	if err := pool.QueryRow(ctx, `
		SELECT job.finished_at IS NOT NULL, cell.checks,
			(SELECT COUNT(*) FROM CheckerActionLogs WHERE team_id = $1 AND task_id = $2),
			(SELECT COUNT(*) FROM TeamTasksLog WHERE team_id = $1 AND task_id = $2)
		FROM CheckerJobs AS job JOIN TeamTasks AS cell USING (team_id, task_id)
		WHERE job.id = $3`, teamID, taskID, jobID).Scan(&finished, &checks, &logs, &snapshots); err != nil {
		t.Fatal(err)
	}
	if finished || checks != 0 || logs != 0 || snapshots != 0 {
		t.Fatalf("failed log write committed result state: finished %t, checks %d, logs %d, snapshots %d", finished, checks, logs, snapshots)
	}
}

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func scheduledGameDocument(t *testing.T) config.Document {
	t.Helper()
	return scheduledGameDocumentWithMode(t, "classic")
}

func scheduledGameDocumentWithMode(t *testing.T, mode string) config.Document {
	t.Helper()
	source := strings.Replace(`game:
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
`, "mode: classic", "mode: "+mode, 1)
	document, err := config.DecodeDocument(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestStartScheduledStartsDueWaitingSessionOnlyOnce(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	repository := postgres.New(pool)
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	if started, err := repository.StartScheduled(ctx, start.Add(-time.Second)); err != nil || started {
		t.Fatalf("early scheduled start = %t, %v; want false, nil", started, err)
	}

	const callers = 8
	started := make(chan bool, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Go(func() {
			changed, err := repository.StartScheduled(ctx, start)
			if err != nil {
				errorsFound <- err
				return
			}
			started <- changed
		})
	}
	wait.Wait()
	close(started)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	var starts int
	for changed := range started {
		if changed {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("concurrent scheduled starts = %d, want exactly one", starts)
	}

	life, err = game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !life.Config.GameRunning || life.Config.RealRound != 0 || life.Session.FinalStart == nil ||
		!life.Session.FinalStart.Equal(start) || life.Session.Generation != 1 {
		t.Fatalf("scheduled session state: %+v", life)
	}
	if err := game.ChangeState(ctx, "finish", nil, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if started, err := repository.StartScheduled(ctx, start.Add(time.Hour)); err != nil || started {
		t.Fatalf("finished session restarted = %t, %v; want false, nil", started, err)
	}
}

func TestConcurrentTickersPrepareOnlyOneRound(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	repository := postgres.New(pool)
	if started, err := repository.StartScheduled(ctx, start); err != nil || !started {
		t.Fatalf("scheduled start = %t, %v", started, err)
	}
	request := domain.ScheduleRequest{
		ScheduleID: "classic_rounds", At: start, StartAt: start,
		Interval: time.Minute, AdvancesRound: true, Workflow: domain.JobActionClassic,
	}
	const callers = 8
	runs := make(chan *domain.DispatchRun, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Go(func() {
			run, err := repository.Prepare(ctx, request)
			if err != nil {
				errorsFound <- err
				return
			}
			runs <- run
		})
	}
	wait.Wait()
	close(runs)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	var runID uuid.UUID
	for run := range runs {
		if run == nil || run.Round != 1 {
			t.Fatalf("concurrent ticker received run %+v; want round 1", run)
		}
		if runID == uuid.Nil() {
			runID = run.ID
		} else if run.ID != runID {
			t.Fatalf("concurrent tickers created separate runs %q and %q", runID, run.ID)
		}
	}
	var round, runCount int32
	if err := pool.QueryRow(ctx, `SELECT real_round,(SELECT count(*) FROM DispatchRuns WHERE schedule_id='classic_rounds')
		FROM GameConfig WHERE id=1`).Scan(&round, &runCount); err != nil {
		t.Fatal(err)
	}
	if round != 1 || runCount != 1 {
		t.Fatalf("concurrent scheduling advanced round/run count to %d/%d, want 1/1", round, runCount)
	}
}

func TestPracticeResetAutomaticallyStartsScheduledFinal(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teamIDs, taskIDs := gameEntityIDs(ctx, t, game)
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	if err := game.ChangeState(ctx, "start_practice", nil, start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	practiceRows := []struct {
		statement string
		args      []any
	}{
		{`INSERT INTO Flags(flag,team_id,task_id,round,public_flag_data,private_flag_data)
		 VALUES('PRACTICE1',$1,$2,1,'practice-public','practice-private')`, []any{teamIDs[0], taskIDs[0]}},
		{`INSERT INTO TeamTasksLog(round,team_id,task_id,status,public_message,private_message,command)
		 VALUES(1,$1,$2,101,'practice-log','practice-private-log','practice-command')`, []any{teamIDs[0], taskIDs[0]}},
		{`INSERT INTO GameRoundSnapshots(round,round_start,snapshot)
		 VALUES(1,0,'{"team_tasks":[]}'::JSONB)`, nil},
		{`INSERT INTO ScheduleHistory(id,last_run) VALUES('practice_test',now())`, nil},
		{`INSERT INTO PublicAnnouncements(title,markdown,author)
			 VALUES('Practice update','Practice closes soon.','admin')`, nil},
	}
	for _, row := range practiceRows {
		if _, err := pool.Exec(ctx, row.statement, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	repository := postgres.New(pool)
	reset, err := repository.Maintain(ctx, start)
	if err != nil || !reset {
		t.Fatalf("practice maintenance reset = %t, %v; want true, nil", reset, err)
	}
	var practiceFlags, practiceLogs, practiceSnapshots, practiceSchedules, practiceAnnouncements, teamCount, taskCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM Flags), (SELECT count(*) FROM TeamTasksLog),
		(SELECT count(*) FROM GameRoundSnapshots), (SELECT count(*) FROM ScheduleHistory),
		(SELECT count(*) FROM PublicAnnouncements),
		(SELECT count(*) FROM Teams), (SELECT count(*) FROM Tasks)`).
		Scan(&practiceFlags, &practiceLogs, &practiceSnapshots, &practiceSchedules, &practiceAnnouncements, &teamCount, &taskCount); err != nil {
		t.Fatal(err)
	}
	if practiceFlags != 0 || practiceLogs != 0 || practiceSnapshots != 0 || practiceSchedules != 0 || practiceAnnouncements != 0 || teamCount != 1 || taskCount != 1 {
		t.Fatalf("practice cleanup kept flags/logs/snapshots/schedules/announcements=%d/%d/%d/%d/%d, teams/tasks=%d/%d",
			practiceFlags, practiceLogs, practiceSnapshots, practiceSchedules, practiceAnnouncements, teamCount, taskCount)
	}
	started, err := repository.StartScheduled(ctx, start)
	if err != nil || !started {
		t.Fatalf("scheduled final start after practice = %t, %v; want true, nil", started, err)
	}
	life, err = game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !life.Config.GameRunning || life.Session.PracticeStart != nil || life.Session.FinalStart == nil ||
		!life.Session.FinalStart.Equal(start) || life.Session.Generation != 3 {
		t.Fatalf("post-practice final state: %+v", life)
	}
	finalRows := []struct {
		statement string
		args      []any
	}{
		{statement: `UPDATE GameConfig SET real_round=1 WHERE id=1`, args: nil},
		{statement: `UPDATE GameSession SET round_started_at=$1,snapshot_round=1,snapshot='{}'::JSONB WHERE id=1`, args: []any{start}},
		{statement: `INSERT INTO Flags(flag,team_id,task_id,round,public_flag_data,private_flag_data)
			VALUES('FINALFLAG',$1,$2,1,'final-public','final-private')`, args: []any{teamIDs[0], taskIDs[0]}},
		{statement: `INSERT INTO TeamTasksLog(round,team_id,task_id,status,public_message,private_message,command)
			VALUES(1,$1,$2,101,'final-log','final-private-log','final-command')`, args: []any{teamIDs[0], taskIDs[0]}},
		{statement: `INSERT INTO GameRoundSnapshots(round,round_start,snapshot)
			VALUES(1,1,'{"team_tasks":[]}'::JSONB)`, args: nil},
	}
	for _, row := range finalRows {
		if _, err := pool.Exec(ctx, row.statement, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := game.ChangeState(ctx, "finish", nil, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Maintain(ctx, start.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var finalFlags, finalLogs, finalSnapshots int
	var finalFlag string
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM Flags),
		(SELECT count(*) FROM TeamTasksLog),(SELECT count(*) FROM GameRoundSnapshots),
		(SELECT flag FROM Flags LIMIT 1)`).
		Scan(&finalFlags, &finalLogs, &finalSnapshots, &finalFlag); err != nil {
		t.Fatal(err)
	}
	if finalFlags != 1 || finalLogs != 1 || finalSnapshots != 1 || finalFlag != "FINALFLAG" {
		t.Fatalf("finished final data was not preserved: flags/logs/snapshots=%d/%d/%d flag=%q",
			finalFlags, finalLogs, finalSnapshots, finalFlag)
	}
}

func TestFinishRequiresStoredScheduledTime(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	runID := uuid.NewV7()
	if _, err := pool.Exec(ctx, `INSERT INTO DispatchRuns(id,schedule_id,round,is_round,round_start,scheduled_at)
		VALUES($1,'missing_scheduled_at',1,FALSE,0,NULL)`, runID); err != nil {
		t.Fatal(err)
	}

	repository := postgres.New(pool)
	finished, err := repository.Finish(ctx, runID, time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil || finished {
		t.Fatalf("Finish() for a run without scheduled_at = %t, %v; want an error", finished, err)
	}
	var completed bool
	var checkpoints int
	if err := pool.QueryRow(ctx, `SELECT completed,(SELECT count(*) FROM ScheduleHistory WHERE id='missing_scheduled_at')
		FROM DispatchRuns WHERE id=$1`, runID).Scan(&completed, &checkpoints); err != nil {
		t.Fatal(err)
	}
	if completed || checkpoints != 0 {
		t.Fatalf("invalid run finish persisted completed=%t checkpoints=%d", completed, checkpoints)
	}
}

func TestSetDeadlineShrinksClaimedJobAndRequiresOwner(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	start := time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := game.ChangeState(ctx, "start_final", nil, start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	runID, jobID := uuid.NewV7(), uuid.NewV7()
	if _, err := pool.Exec(ctx, `INSERT INTO DispatchRuns(id,schedule_id,round,is_round,round_start,scheduled_at)
		VALUES($1,'deadline_test',1,FALSE,0,$2)`, runID, start); err != nil {
		t.Fatal(err)
	}
	originalDeadline := start.Add(10 * time.Minute)
	if _, err := pool.Exec(ctx, `INSERT INTO CheckerJobs
		(id,run_id,round,payload,action,team_id,task_id,sent_at,deadline_at)
		VALUES($1,$2,1,
		'{"generation":1,"task":{"checker_timeout":10}}'::JSONB,
		'classic',$3,$4,$5,$6)`, jobID, runID, teams[0], tasks[0], start, originalDeadline); err != nil {
		t.Fatal(err)
	}
	claimAt := start.Add(time.Second)
	claimDeadline := claimAt.Add(time.Minute)
	repository := postgres.New(pool)
	if _, err := repository.Claim(ctx, jobID, "owner-one", claimAt, claimDeadline); err != nil {
		t.Fatal(err)
	}
	requested := claimAt.Add(20 * time.Second)
	if changed, err := repository.SetDeadline(ctx, jobID, "owner-one", claimAt.Add(time.Second), requested); err != nil || !changed {
		t.Fatalf("set deadline = %t, %v; want true, nil", changed, err)
	}
	var storedDeadline, storedClaimDeadline time.Time
	if err := pool.QueryRow(ctx, `SELECT deadline_at,claim_deadline_at FROM CheckerJobs WHERE id=$1`, jobID).
		Scan(&storedDeadline, &storedClaimDeadline); err != nil {
		t.Fatal(err)
	}
	if !storedDeadline.Equal(requested) || !storedClaimDeadline.Equal(requested) {
		t.Fatalf("stored deadlines = %s, %s; want both %s", storedDeadline, storedClaimDeadline, requested)
	}
	if changed, err := repository.SetDeadline(ctx, jobID, "stale-owner", claimAt.Add(2*time.Second), claimAt.Add(30*time.Second)); err != nil || changed {
		t.Fatalf("stale owner changed deadline = %t, %v; want false, nil", changed, err)
	}
}

func TestExpiredClaimFailsWithoutSLAAccountingOrPUTReplay(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	start := time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := game.ChangeState(ctx, "start_final", nil, start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE TeamTasks SET status=101,score=125,checks=9,checks_passed=7 WHERE team_id=$1 AND task_id=$2`, teams[0], tasks[0]); err != nil {
		t.Fatal(err)
	}
	runID, jobID := uuid.NewV7(), uuid.NewV7()
	if _, err := pool.Exec(ctx, `INSERT INTO DispatchRuns(id,schedule_id,round,is_round,round_start,scheduled_at)
		VALUES($1,'expired_test',1,FALSE,0,$2)`, runID, start); err != nil {
		t.Fatal(err)
	}
	deadline := start.Add(10 * time.Second)
	if _, err := pool.Exec(ctx, `INSERT INTO CheckerJobs
		(id,run_id,round,payload,action,team_id,task_id,sent_at,deadline_at)
		VALUES($1,$2,1,
		jsonb_build_object('generation',1,'team',jsonb_build_object('id',$3::UUID),
		                   'task',jsonb_build_object('id',$4::UUID,'checker_timeout',1)),
		'classic',$3,$4,$5,$6)`, jobID, runID, teams[0], tasks[0], start, deadline); err != nil {
		t.Fatal(err)
	}
	repository := postgres.New(pool)
	claimAt := start.Add(time.Second)
	if _, err := repository.Claim(ctx, jobID, "first-owner", claimAt, claimAt.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Claim(ctx, jobID, "second-owner", start.Add(4*time.Second), start.Add(6*time.Second)); !errors.Is(err, domain.ErrJobClaimed) {
		t.Fatalf("reclaimed a claimed PUT workflow after worker loss: %v", err)
	}
	if expired, err := repository.Expire(ctx, jobID, deadline.Add(time.Second)); err != nil || !expired {
		t.Fatalf("expired job = %t, %v; want true, nil", expired, err)
	}
	var status, checks, passed int32
	var score float64
	var loggedStatus, loggedChecks, loggedPassed int32
	if err := pool.QueryRow(ctx, `SELECT status,checks,checks_passed,score FROM TeamTasks WHERE team_id=$1 AND task_id=$2`, teams[0], tasks[0]).
		Scan(&status, &checks, &passed, &score); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status,checks,checks_passed FROM TeamTasksLog WHERE round=1 AND team_id=$1 AND task_id=$2`, teams[0], tasks[0]).
		Scan(&loggedStatus, &loggedChecks, &loggedPassed); err != nil {
		t.Fatal(err)
	}
	if status != domain.TaskStatusCheckFailed || checks != 9 || passed != 7 || score != 125 ||
		loggedStatus != 101 || loggedChecks != 9 || loggedPassed != 7 {
		t.Fatalf("expired workflow changed SLA/score: current=%d %d/%d score=%v history=%d %d/%d",
			status, passed, checks, score, loggedStatus, loggedPassed, loggedChecks)
	}
}

func TestBlitzSeparatesPutsAndUsesTaskGetPeriod(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocumentWithMode(t, "blitz")); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	if _, err := pool.Exec(ctx, `UPDATE Tasks SET gets=1,puts=1,get_period=30 WHERE id=$1`, tasks[0]); err != nil {
		t.Fatal(err)
	}
	var readOnlyTask uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO Tasks
		(name,checker,gets,puts,places,checker_timeout,checker_type,get_period,default_score,active)
		VALUES('read-only','example/checker.py',1,0,1,10,'hackerdom',30,2500,TRUE) RETURNING id`).
		Scan(&readOnlyTask); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO TeamTasks(team_id,task_id,status,score) VALUES($1,$2,-1,2500)`, teams[0], readOnlyTask); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	repository := postgres.New(pool)
	if started, err := repository.StartScheduled(ctx, start); err != nil || !started {
		t.Fatalf("scheduled start = %t, %v", started, err)
	}
	puts, err := repository.Prepare(ctx, domain.ScheduleRequest{
		ScheduleID: "blitz_rounds", At: start, StartAt: start,
		Interval: time.Minute, AdvancesRound: true, Workflow: domain.JobActionPutsOnly,
	})
	if err != nil || puts == nil {
		t.Fatalf("prepare blitz puts = %+v, %v", puts, err)
	}
	var putsCount int
	var putsAction, putsWorkflow string
	if err := pool.QueryRow(ctx, `SELECT count(*),min(action),min(payload->>'workflow')
		FROM CheckerJobs WHERE run_id=$1`, puts.ID).Scan(&putsCount, &putsAction, &putsWorkflow); err != nil {
		t.Fatal(err)
	}
	if putsCount != 1 || putsAction != string(domain.JobActionPutsOnly) || putsWorkflow != string(domain.JobActionPutsOnly) {
		t.Fatalf("blitz PUT jobs = %d, action/workflow %q/%q", putsCount, putsAction, putsWorkflow)
	}

	taskID := tasks[0]
	scheduleID := "blitz_check_gets_task_" + taskID.String()
	if len(scheduleID) <= 32 {
		t.Fatalf("blitz schedule ID length = %d, want more than 32", len(scheduleID))
	}
	getRequest := domain.ScheduleRequest{
		ScheduleID: scheduleID, At: start, StartAt: start,
		Interval: 30 * time.Second, Workflow: domain.JobActionCheckGets, TaskID: &taskID,
	}
	gets, err := repository.Prepare(ctx, getRequest)
	if err != nil || gets == nil {
		t.Fatalf("prepare blitz CHECK/GET = %+v, %v", gets, err)
	}
	var getsAction, getsWorkflow string
	if err := pool.QueryRow(ctx, `SELECT action,payload->>'workflow' FROM CheckerJobs WHERE run_id=$1`, gets.ID).
		Scan(&getsAction, &getsWorkflow); err != nil {
		t.Fatal(err)
	}
	if getsAction != string(domain.JobActionCheckGets) || getsWorkflow != string(domain.JobActionCheckGets) {
		t.Fatalf("blitz CHECK/GET action/workflow = %q/%q", getsAction, getsWorkflow)
	}
	if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET sent_at=$1,finished_at=$1 WHERE run_id=$2`, start, gets.ID); err != nil {
		t.Fatal(err)
	}
	if finished, err := repository.Finish(ctx, gets.ID, start); err != nil || !finished {
		t.Fatalf("finish blitz CHECK/GET = %t, %v", finished, err)
	}
	checkpoint, err := repository.LastRun(ctx, scheduleID)
	if err != nil || checkpoint == nil || !checkpoint.Equal(start) {
		t.Fatalf("long blitz schedule checkpoint = %v, %v; want %s", checkpoint, err, start)
	}
	getRequest.At = start.Add(29 * time.Second)
	tooSoon, err := repository.Prepare(ctx, getRequest)
	if err != nil || tooSoon != nil {
		t.Fatalf("blitz CHECK/GET before get_period = %+v, %v", tooSoon, err)
	}
	getRequest.At = start.Add(30 * time.Second)
	next, err := repository.Prepare(ctx, getRequest)
	if err != nil || next == nil || next.Workflow != domain.JobActionCheckGets {
		t.Fatalf("blitz CHECK/GET at get_period = %+v, %v", next, err)
	}
}

func TestPrepareRechecksPersistedDueTimeAfterRunFinishes(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	repository := postgres.New(pool)
	if started, err := repository.StartScheduled(ctx, start); err != nil || !started {
		t.Fatalf("scheduled start = %t, %v", started, err)
	}
	request := domain.ScheduleRequest{
		ScheduleID: "classic_rounds", At: start, StartAt: start,
		Interval: time.Minute, AdvancesRound: true, Workflow: domain.JobActionClassic,
	}
	first, err := repository.Prepare(ctx, request)
	if err != nil || first == nil {
		t.Fatalf("prepare first run = %+v, %v", first, err)
	}
	publicSnapshot, err := json.Marshal(first.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, privateKey := range []string{"public_message", "private_message", "command"} {
		if strings.Contains(string(publicSnapshot), `"`+privateKey+`"`) {
			t.Fatalf("public round snapshot contains private key %q: %s", privateKey, publicSnapshot)
		}
	}
	if !strings.Contains(string(publicSnapshot), `"message"`) {
		t.Fatalf("public round snapshot has no message field: %s", publicSnapshot)
	}
	request.At = start.Add(time.Minute)
	waiting, err := repository.Prepare(ctx, request)
	if err != nil || waiting == nil || waiting.ID != first.ID || waiting.Round != first.Round {
		t.Fatalf("classic round advanced before prior job finished: %+v, %v", waiting, err)
	}
	life, err = game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if life.Config.RealRound != 1 {
		t.Fatalf("classic round while prior job unfinished = %d, want 1", life.Config.RealRound)
	}
	if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET sent_at=$1,finished_at=$1 WHERE run_id=$2`, start, first.ID); err != nil {
		t.Fatal(err)
	}
	if finished, err := repository.Finish(ctx, first.ID, start); err != nil || !finished {
		t.Fatalf("finish first run = %t, %v", finished, err)
	}

	request.At = start
	stale, err := repository.Prepare(ctx, request)
	if err != nil || stale != nil {
		t.Fatalf("stale prepare after finish = %+v, %v; want nil, nil", stale, err)
	}
	request.At = start.Add(59 * time.Second)
	tooSoon, err := repository.Prepare(ctx, request)
	if err != nil || tooSoon != nil {
		t.Fatalf("prepare before interval = %+v, %v; want nil, nil", tooSoon, err)
	}
	request.At = start.Add(time.Minute)
	next, err := repository.Prepare(ctx, request)
	if err != nil || next == nil || next.Round != 2 {
		t.Fatalf("prepare at next interval = %+v, %v; want round 2", next, err)
	}
}

func TestPrepareUsesPersistedPauseClockForDueBoundary(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, scheduledGameDocument(t)); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := life.Config.StartTime
	repository := postgres.New(pool)
	if started, err := repository.StartScheduled(ctx, start); err != nil || !started {
		t.Fatalf("scheduled start = %t, %v", started, err)
	}
	request := domain.ScheduleRequest{
		ScheduleID: "classic_rounds", At: start, StartAt: start,
		Interval: time.Minute, AdvancesRound: true, Workflow: domain.JobActionClassic,
	}
	run, err := repository.Prepare(ctx, request)
	if err != nil || run == nil {
		t.Fatalf("prepare first run = %+v, %v", run, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE CheckerJobs SET sent_at=$1,finished_at=$1 WHERE run_id=$2`, start, run.ID); err != nil {
		t.Fatal(err)
	}
	if finished, err := repository.Finish(ctx, run.ID, start); err != nil || !finished {
		t.Fatalf("finish first run = %t, %v", finished, err)
	}
	if err := game.ChangeState(ctx, "pause", nil, start.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := game.ChangeState(ctx, "resume", nil, start.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	life, err = game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if life.Session.SchedulePausedSeconds != 20 {
		t.Fatalf("persisted schedule pause = %v, want 20 seconds", life.Session.SchedulePausedSeconds)
	}
	request.At = start.Add(79 * time.Second)
	tooSoon, err := repository.Prepare(ctx, request)
	if err != nil || tooSoon != nil {
		t.Fatalf("paused-clock schedule ran early = %+v, %v", tooSoon, err)
	}
	request.At = start.Add(80 * time.Second)
	next, err := repository.Prepare(ctx, request)
	if err != nil || next == nil || next.Round != 2 {
		t.Fatalf("paused-clock schedule missed its due boundary = %+v, %v", next, err)
	}
}

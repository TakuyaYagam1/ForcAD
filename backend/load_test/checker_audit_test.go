package load_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type checkerRoundReport struct {
	Round                  int     `json:"round"`
	Started                string  `json:"started"`
	Finished               *string `json:"finished"`
	Jobs                   int     `json:"jobs"`
	Cells                  int     `json:"cells"`
	FinishedJobs           int     `json:"finished_jobs"`
	UpJobs                 int     `json:"up_jobs"`
	PutActions             int     `json:"put_actions"`
	StoredFlags            int     `json:"stored_flags"`
	MismatchedCells        int     `json:"mismatched_cells"`
	Complete               bool    `json:"complete"`
	CompletionSeconds      float64 `json:"completion_seconds"`
	FirstActionDelay       float64 `json:"first_action_delay_seconds"`
	MaxPublishToActionWait float64 `json:"max_publish_to_action_wait_seconds"`
	MaxActionMS            int64   `json:"max_action_ms"`
}

type checkerReport struct {
	Time           string               `json:"time"`
	RoundSeconds   int                  `json:"round_seconds"`
	ExpectedCells  int                  `json:"expected_cells"`
	ExpectedPuts   int                  `json:"expected_puts"`
	Tasks          json.RawMessage      `json:"tasks"`
	FailedJobs     json.RawMessage      `json:"failed_jobs"`
	FailedActions  json.RawMessage      `json:"failed_actions"`
	BadJobs        int                  `json:"bad_jobs"`
	BadActions     int                  `json:"bad_actions"`
	ActionTimeouts int                  `json:"action_timeouts"`
	ExpiredJobs    int                  `json:"expired_jobs"`
	OverdueJobs    int                  `json:"overdue_jobs"`
	PastRoundOpen  int                  `json:"past_round_open_jobs"`
	Rounds         []checkerRoundReport `json:"rounds"`
}

// This fixture uses classic mode with no extra checks: one durable job per
// team/task/round. TeamTasksLog contains the prior cell state, not that job's
// result; DispatchRuns.completed only means publication has finished.
const checkerAuditSQL = `WITH
runs AS (SELECT * FROM DispatchRuns WHERE is_round),
j AS (SELECT job.*, run.round_start FROM CheckerJobs job JOIN runs run ON run.id=job.run_id),
jc AS (SELECT round, team_id, task_id, count(*) AS jobs,
 count(*) FILTER (WHERE finished_at IS NOT NULL) AS finished,
 count(*) FILTER (WHERE finished_at IS NOT NULL AND (result->>'status')::integer=101) AS up,
 min(sent_at) AS sent FROM j GROUP BY round,team_id,task_id),
ac AS (SELECT round, team_id, task_id,
 count(*) FILTER (WHERE action='check') AS checks,
 count(*) FILTER (WHERE action='put') AS puts,
 count(*) FILTER (WHERE status<>101 OR timed_out) AS bad,
 min(started_at) AS first_action, max(duration_ms) AS max_action_ms
 FROM CheckerActionLogs GROUP BY round,team_id,task_id),
fc AS (SELECT round,team_id,task_id,count(*) AS flags FROM Flags GROUP BY round,team_id,task_id),
cells AS (SELECT run.id,run.round,team.id AS team_id,task.id AS task_id,task.puts,
 COALESCE(jc.jobs,0) AS jobs,COALESCE(jc.finished,0) AS finished,COALESCE(jc.up,0) AS up,
 COALESCE(ac.puts,0) AS actual_puts,COALESCE(fc.flags,0) AS flags,
 (COALESCE(jc.jobs,0)<>1 OR COALESCE(jc.finished,0)<>1 OR COALESCE(jc.up,0)<>1
 OR COALESCE(ac.checks,0)<>1 OR COALESCE(ac.puts,0)<>task.puts
 OR COALESCE(ac.bad,0)<>0 OR COALESCE(fc.flags,0)<>task.puts) AS mismatch,
 ac.first_action,ac.max_action_ms,extract(epoch FROM ac.first_action-jc.sent) AS publish_wait
 FROM runs run CROSS JOIN Teams team CROSS JOIN Tasks task
 LEFT JOIN jc ON jc.round=run.round AND jc.team_id=team.id AND jc.task_id=task.id
 LEFT JOIN ac ON ac.round=run.round AND ac.team_id=team.id AND ac.task_id=task.id
 LEFT JOIN fc ON fc.round=run.round AND fc.team_id=team.id AND fc.task_id=task.id
 WHERE team.active AND task.active),
round_cells AS (SELECT id,sum(jobs) AS jobs,count(*) FILTER (WHERE jobs>0) AS cells,
 sum(finished) AS finished_jobs,sum(up) AS up_jobs,sum(actual_puts) AS put_actions,
 sum(flags) AS stored_flags,count(*) FILTER (WHERE mismatch) AS mismatched_cells,
 min(first_action) AS first_action,max(max_action_ms) AS max_action_ms,
 max(publish_wait) AS publish_wait FROM cells GROUP BY id),
round_jobs AS (SELECT run_id,count(*) AS jobs,count(*) FILTER (WHERE finished_at IS NOT NULL) AS finished,
 max(finished_at) AS finished_at FROM j GROUP BY run_id),
rounds AS (SELECT run.round,to_timestamp(run.round_start) AS started,
 CASE WHEN rj.finished=rj.jobs THEN rj.finished_at END AS finished,
 rc.jobs,rc.cells,rc.finished_jobs,rc.up_jobs,rc.put_actions,rc.stored_flags,rc.mismatched_cells,
 COALESCE(rj.jobs>0 AND rj.finished=rj.jobs,FALSE) AS complete,
 COALESCE(extract(epoch FROM rj.finished_at)-run.round_start,0) AS completion_seconds,
 COALESCE(extract(epoch FROM rc.first_action)-run.round_start,0) AS first_action_delay_seconds,
 COALESCE(rc.publish_wait,0) AS max_publish_to_action_wait_seconds,
 COALESCE(rc.max_action_ms,0) AS max_action_ms
 FROM runs run LEFT JOIN round_cells rc ON rc.id=run.id LEFT JOIN round_jobs rj ON rj.run_id=run.id)
SELECT json_build_object('time',statement_timestamp(),
 'round_seconds',(SELECT round_time FROM GameConfig),
 'expected_cells',(SELECT count(*) FROM Teams WHERE active)*(SELECT count(*) FROM Tasks WHERE active),
 'expected_puts',(SELECT count(*) FROM Teams WHERE active)*(SELECT sum(puts) FROM Tasks WHERE active),
 'tasks',(SELECT json_agg(t ORDER BY name) FROM (SELECT name,puts,places,gets,checker_timeout FROM Tasks WHERE active) t),
 'bad_jobs',(SELECT count(*) FROM j WHERE finished_at IS NOT NULL AND COALESCE((result->>'status')::integer,0)<>101),
 'bad_actions',(SELECT count(*) FROM CheckerActionLogs WHERE status<>101),
 'action_timeouts',(SELECT count(*) FROM CheckerActionLogs WHERE timed_out),
 'expired_jobs',(SELECT count(*) FROM j WHERE result->>'private_message'='Execution or queue deadline expired'),
 'overdue_jobs',(SELECT count(*) FROM j WHERE finished_at IS NULL AND deadline_at<=statement_timestamp()),
 'past_round_open_jobs',(SELECT count(*) FROM j WHERE finished_at IS NULL
   AND round_start+(SELECT round_time FROM GameConfig)<extract(epoch FROM statement_timestamp())),
 'failed_jobs',COALESCE((SELECT json_agg(f) FROM (
   SELECT job.id,job.round,team.name AS team,task.name AS service,
   job.sent_at,job.finished_at,job.result->'status' AS status,
   job.result->>'public_message' AS public_message,
   left(job.result->>'private_message',16384) AS private_message,
   job.result->>'command' AS command
   FROM j job JOIN Teams team ON team.id=job.team_id JOIN Tasks task ON task.id=job.task_id
   WHERE job.finished_at IS NOT NULL AND COALESCE((job.result->>'status')::integer,0)<>101
   ORDER BY job.finished_at,job.id LIMIT 100) f),'[]'),
 'failed_actions',COALESCE((SELECT json_agg(f) FROM (
   SELECT log.id,log.round,team.name AS team,task.name AS service,log.action,log.stage,
   log.started_at,log.finished_at,log.duration_ms,log.timed_out,log.status,log.public_message
   FROM CheckerActionLogs log JOIN Teams team ON team.id=log.team_id JOIN Tasks task ON task.id=log.task_id
   WHERE log.status<>101 OR log.timed_out ORDER BY log.finished_at,log.id LIMIT 100) f),'[]'),
 'rounds',COALESCE((SELECT json_agg(rounds ORDER BY round) FROM rounds),'[]'))`

func collectCheckerReport(ctx context.Context, d *deployment) (*checkerReport, error) {
	data, err := d.query(ctx, checkerAuditSQL)
	if err != nil {
		return nil, err
	}
	var report checkerReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("load.collectCheckerReport - decode: %w", err)
	}
	return &report, nil
}

func (r *checkerReport) completedRounds() int {
	count := 0
	for _, round := range r.Rounds {
		if round.Complete {
			count++
		}
	}
	return count
}

func (r *checkerReport) validate() error {
	if r.RoundSeconds <= 0 || r.ExpectedCells != teamCount*3 || r.ExpectedPuts < r.ExpectedCells || len(r.Rounds) == 0 {
		return fmt.Errorf("load.checkerAudit - missing or unexpected fixture dimensions")
	}
	if r.BadJobs+r.BadActions+r.ActionTimeouts+r.ExpiredJobs+r.OverdueJobs+r.PastRoundOpen != 0 {
		return fmt.Errorf("load.checkerAudit - bad jobs=%d actions=%d timeouts=%d expired=%d overdue=%d jobs beyond round=%d", r.BadJobs, r.BadActions, r.ActionTimeouts, r.ExpiredJobs, r.OverdueJobs, r.PastRoundOpen)
	}
	for index, round := range r.Rounds {
		if round.Round != index+1 || round.Jobs != r.ExpectedCells || round.Cells != r.ExpectedCells {
			return fmt.Errorf("load.checkerAudit - round %d has missing/duplicate jobs or missing round: jobs=%d cells=%d", round.Round, round.Jobs, round.Cells)
		}
		if !round.Complete {
			continue // Only the current round may still be in flight within its 60s window.
		}
		if round.FinishedJobs != r.ExpectedCells || round.UpJobs != r.ExpectedCells || round.PutActions != r.ExpectedPuts || round.StoredFlags != r.ExpectedPuts || round.MismatchedCells != 0 {
			return fmt.Errorf("load.checkerAudit - round %d incomplete results: finished=%d up=%d puts=%d flags=%d mismatched cells=%d", round.Round, round.FinishedJobs, round.UpJobs, round.PutActions, round.StoredFlags, round.MismatchedCells)
		}
		if round.CompletionSeconds > float64(r.RoundSeconds) {
			return fmt.Errorf("load.checkerAudit - round %d completion %.3fs exceeded %ds", round.Round, round.CompletionSeconds, r.RoundSeconds)
		}
	}
	return nil
}

// A partial interval at either boundary need not contain a completed round.
func minimumCompletedRounds(duration time.Duration, roundSeconds int) int {
	if roundSeconds <= 0 {
		return 0
	}
	return max(0, int(duration/(time.Duration(roundSeconds)*time.Second))-1)
}

const completedCheckerRoundsSQL = `SELECT count(*) FROM (
 SELECT run.id FROM DispatchRuns run JOIN CheckerJobs job ON job.run_id=run.id
 WHERE run.is_round GROUP BY run.id HAVING bool_and(job.finished_at IS NOT NULL)
) completed`

const checkerLifecycleSQL = `SELECT json_build_object('time',statement_timestamp(),
 'round',(SELECT real_round FROM GameConfig),
 'open',count(*) FILTER (WHERE finished_at IS NULL),
 'unsent',count(*) FILTER (WHERE finished_at IS NULL AND sent_at IS NULL),
 'claimed',count(*) FILTER (WHERE finished_at IS NULL AND claim_token IS NOT NULL),
 'overdue',count(*) FILTER (WHERE finished_at IS NULL AND deadline_at<=statement_timestamp()),
 'expired',count(*) FILTER (WHERE result->>'private_message'='Execution or queue deadline expired'),
 'bad_finished',count(*) FILTER (WHERE finished_at IS NOT NULL AND COALESCE((result->>'status')::integer,0)<>101),
 'oldest_open_seconds',COALESCE(max(extract(epoch FROM statement_timestamp())-run.round_start) FILTER (WHERE finished_at IS NULL),0))
 FROM CheckerJobs job JOIN DispatchRuns run ON run.id=job.run_id WHERE run.is_round`

func (o *observers) sampleCheckerLifecycle(ctx context.Context, d *deployment, directory string) {
	file, err := os.OpenFile(filepath.Join(directory, "checker-lifecycle.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		o.fail(err)
		return
	}
	defer file.Close()
	for ctx.Err() == nil {
		data, err := d.query(ctx, checkerLifecycleSQL)
		if err == nil {
			_, err = file.Write(data)
		}
		if err != nil {
			if ctx.Err() == nil {
				o.fail(fmt.Errorf("load.sampleCheckerLifecycle: %w", err))
			}
			return
		}
		if !waitContext(ctx, 10*time.Second) {
			return
		}
	}
}

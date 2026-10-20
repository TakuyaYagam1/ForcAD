-- name: LockSchedulerGame :one
SELECT COALESCE(real_round, 0)::INTEGER AS real_round,
       COALESCE(game_running, FALSE)::BOOLEAN AS game_running,
       rounds, COALESCE(mode, 'classic')::TEXT AS mode, start_time,
       COALESCE(round_time, 60)::INTEGER AS round_time, extra_checks
FROM GameConfig
WHERE id = 1
FOR UPDATE;

-- name: LockSchedulerSession :one
SELECT practice_start, final_start, reset_pending, generation, paused_at,
       paused_seconds, schedule_paused_seconds, round_started_at,
       snapshot_round, snapshot
FROM GameSession
WHERE id = 1
FOR UPDATE;

-- name: StartScheduledGame :execrows
UPDATE GameConfig
SET game_running = TRUE
WHERE id = 1
  AND NOT game_running
  AND real_round = 0
  AND start_time IS NOT NULL
  AND start_time <= sqlc.arg(at)
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1
        AND session.practice_start IS NULL
        AND session.final_start IS NULL
        AND NOT session.reset_pending
        AND session.paused_at IS NULL
  );

-- name: StartScheduledFinalSession :execrows
UPDATE GameSession
SET final_start = sqlc.arg(started_at),
    paused_at = NULL,
    paused_seconds = 0,
    schedule_paused_seconds = 0,
    generation = generation + 1
WHERE id = 1
  AND practice_start IS NULL
  AND final_start IS NULL
  AND NOT reset_pending
  AND paused_at IS NULL;

-- name: PendingSchedulerRun :one
SELECT id, schedule_id, round, is_round, round_start, snapshot, completed,
       snapshot_round, snapshot_round_start, scheduled_at
FROM DispatchRuns
WHERE schedule_id = sqlc.arg(schedule_id) AND NOT completed
FOR UPDATE;

-- name: HasSchedulerJobsThroughRound :one
SELECT EXISTS (
    SELECT 1 FROM CheckerJobs
    WHERE round <= sqlc.arg(round) AND finished_at IS NULL
)::BOOLEAN AS has_unfinished;

-- name: GetSchedulerTeamTaskSnapshot :one
SELECT COALESCE(
    jsonb_agg(to_jsonb(cell) ORDER BY cell.team_id, cell.task_id),
    '[]'::JSONB
)::TEXT AS snapshot
FROM TeamTasks AS cell
JOIN Teams AS team ON team.id = cell.team_id
JOIN Tasks AS task ON task.id = cell.task_id
WHERE team.active AND task.active;

-- name: CreateSchedulerRun :one
INSERT INTO DispatchRuns (
    id, schedule_id, round, is_round, round_start, snapshot,
    snapshot_round, snapshot_round_start, scheduled_at
)
VALUES (
    sqlc.arg(id), sqlc.arg(schedule_id), sqlc.arg(round), sqlc.arg(is_round),
    sqlc.arg(round_start), sqlc.arg(snapshot)::JSONB,
    sqlc.narg(snapshot_round), sqlc.narg(snapshot_round_start), sqlc.arg(scheduled_at)
)
RETURNING id, schedule_id, round, is_round, round_start, snapshot, completed,
          snapshot_round, snapshot_round_start, scheduled_at;

-- name: InsertSchedulerJobs :exec
INSERT INTO CheckerJobs (
    id, run_id, round, payload, action, team_id, task_id, not_before,
    expires_at, deadline_at, aggregate_round
)
SELECT job.id, job.run_id, job.round, job.payload, job.action, job.team_id, job.task_id,
       job.not_before, job.expires_at, job.expires_at AS deadline_at, job.aggregate_round
FROM jsonb_to_recordset(sqlc.arg(jobs)::JSONB) AS job(
    id UUID, run_id UUID, round INTEGER, payload JSONB, action VARCHAR(16),
    team_id UUID, task_id UUID, not_before TIMESTAMP WITH TIME ZONE,
    expires_at TIMESTAMP WITH TIME ZONE, aggregate_round BOOLEAN
);

-- name: SetSchedulerRound :execrows
UPDATE GameConfig
SET real_round = sqlc.arg(round)
WHERE id = 1;

-- name: StopAtRoundLimit :execrows
UPDATE GameConfig
SET game_running = FALSE
WHERE id = 1 AND game_running;

-- name: SaveSchedulerRoundRuntime :execrows
UPDATE GameSession
SET round_started_at = sqlc.arg(round_started_at),
    paused_seconds = 0,
    snapshot_round = sqlc.arg(snapshot_round),
    snapshot = sqlc.arg(snapshot)::JSONB
WHERE id = 1;

-- name: PendingSchedulerJobs :many
SELECT id, run_id, round, action, team_id, task_id, payload, sent_at,
       publish_token, publish_deadline_at, claim_token, claim_deadline_at,
       put_started_at, finished_at, deadline_at
FROM CheckerJobs
WHERE run_id = sqlc.arg(run_id)
  AND sent_at IS NULL AND finished_at IS NULL
ORDER BY id
LIMIT sqlc.arg(job_limit);

-- name: InsertCompletedRoundSnapshot :execrows
INSERT INTO GameRoundSnapshots (round, round_start, snapshot)
VALUES (sqlc.arg(round), sqlc.arg(round_start), sqlc.arg(snapshot)::JSONB)
ON CONFLICT (round) DO NOTHING;

-- name: ListCompletedRoundSnapshots :many
SELECT round, round_start, snapshot
FROM GameRoundSnapshots
WHERE round > sqlc.arg(after_round)
ORDER BY round
LIMIT sqlc.arg(snapshot_limit);

-- name: GetSchedulerLastRun :one
SELECT last_run
FROM ScheduleHistory
WHERE id = sqlc.arg(schedule_id);

-- name: IsSchedulerScheduleDue :one
SELECT sqlc.arg(schedule_at)::TIMESTAMPTZ >= sqlc.arg(start_at)::TIMESTAMPTZ
   AND COALESCE(
       (
           SELECT last_run IS NULL OR
               last_run + sqlc.arg(interval_seconds)::DOUBLE PRECISION * INTERVAL '1 second'
                   <= sqlc.arg(schedule_at)::TIMESTAMPTZ
           FROM ScheduleHistory
           WHERE id = sqlc.arg(schedule_id)
       ),
       TRUE
   )::BOOLEAN AS due;

-- name: ExpiredSchedulerJobs :many
SELECT id
FROM CheckerJobs
WHERE finished_at IS NULL AND deadline_at <= sqlc.arg(at)
  AND (action IS DISTINCT FROM 'check_only' OR EXISTS (
      SELECT 1 FROM GameSession WHERE id = 1 AND paused_at IS NULL
  ))
ORDER BY deadline_at, id
LIMIT sqlc.arg(job_limit);

-- name: HasUnfinishedSchedulerJobs :one
SELECT EXISTS (
    SELECT 1 FROM CheckerJobs WHERE finished_at IS NULL
)::BOOLEAN AS has_unfinished;

-- name: StopPracticeSession :exec
UPDATE GameConfig SET game_running = FALSE WHERE id = 1;

-- name: CancelPracticeOutbox :exec
UPDATE CheckerJobs
SET finished_at = sqlc.arg(at)
WHERE sent_at IS NULL AND finished_at IS NULL;

-- name: MarkPracticeResetPending :exec
UPDATE GameSession SET reset_pending = TRUE WHERE id = 1;

-- name: ResetPracticeTeamTasks :exec
UPDATE TeamTasks AS cell
SET status = -1, stolen = 0, lost = 0, score = COALESCE(task.default_score, 0),
    checks = 0, checks_passed = 0,
    public_message = '', private_message = '', command = ''
FROM Tasks AS task
WHERE task.id = cell.task_id;

-- name: DeletePracticeRuntime :exec
DELETE FROM GameRoundSnapshots;

-- name: DeletePracticeJobs :exec
DELETE FROM CheckerJobs;

-- name: DeletePracticeRuns :exec
DELETE FROM DispatchRuns;

-- name: DeletePracticeStolenFlags :exec
DELETE FROM StolenFlags;

-- name: DeletePracticeFlags :exec
DELETE FROM Flags;

-- name: DeletePracticeHistory :exec
DELETE FROM TeamTasksLog;

-- name: DeletePracticeScheduleHistory :exec
DELETE FROM ScheduleHistory;

-- name: ResetPracticeGame :exec
UPDATE GameConfig SET real_round = 0, game_running = FALSE WHERE id = 1;

-- name: ResetPracticeSession :exec
UPDATE GameSession
SET practice_start = NULL, reset_pending = FALSE, generation = generation + 1,
    paused_at = NULL, paused_seconds = 0, schedule_paused_seconds = 0,
    round_started_at = NULL, snapshot_round = NULL, snapshot = NULL
WHERE id = 1;

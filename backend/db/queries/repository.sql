-- name: ActiveTeamIDByToken :one
SELECT id
FROM Teams
WHERE token = $1 AND active = TRUE;

-- name: IsActiveTeamByID :one
SELECT COALESCE(active, FALSE)::BOOLEAN AS active
FROM Teams
WHERE id = sqlc.arg(team_id);

-- name: GetFlagByValue :one
SELECT id, flag, team_id, task_id, round, public_flag_data, private_flag_data, vuln_number
FROM Flags
WHERE flag = $1;

-- name: GetFlagByID :one
SELECT id, flag, team_id, task_id, round, public_flag_data, private_flag_data, vuln_number
FROM Flags
WHERE id = $1;

-- name: GetLiveFlagByValue :one
SELECT f.id, f.flag, f.team_id, f.task_id, f.round,
       f.public_flag_data, f.private_flag_data, f.vuln_number
FROM Flags f
JOIN GameConfig gc ON gc.id = 1
WHERE f.flag = sqlc.arg(flag)
  AND f.round::BIGINT >= sqlc.arg(current_round)::BIGINT - COALESCE(gc.flag_lifetime, 0)::BIGINT;

-- name: GetLiveFlagByID :one
SELECT f.id, f.flag, f.team_id, f.task_id, f.round,
       f.public_flag_data, f.private_flag_data, f.vuln_number
FROM Flags f
JOIN GameConfig gc ON gc.id = 1
WHERE f.id = sqlc.arg(id)
  AND f.round::BIGINT >= sqlc.arg(current_round)::BIGINT - COALESCE(gc.flag_lifetime, 0)::BIGINT;

-- name: GetLiveFlagsByValues :many
SELECT f.id, f.flag, f.team_id, f.task_id, f.round,
       f.public_flag_data, f.private_flag_data, f.vuln_number
FROM Flags f
JOIN GameConfig gc ON gc.id = 1
WHERE f.flag = ANY(sqlc.arg(flag_values)::VARCHAR[])
  AND f.round::BIGINT >= sqlc.arg(current_round)::BIGINT - COALESCE(gc.flag_lifetime, 0)::BIGINT;

-- name: GetRandomRoundFlag :one
SELECT id, flag, team_id, task_id, round, public_flag_data, private_flag_data, vuln_number
FROM Flags
WHERE team_id = sqlc.arg(team_id)
  AND task_id = sqlc.arg(task_id)
  AND round = sqlc.arg(round)
ORDER BY RANDOM()
LIMIT 1;

-- name: InsertFlag :one
INSERT INTO Flags (flag, team_id, task_id, round, public_flag_data, private_flag_data, vuln_number)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, flag, team_id, task_id, round, public_flag_data, private_flag_data, vuln_number;

-- name: GetFlagLifetime :one
SELECT COALESCE(flag_lifetime, 0)::INTEGER AS flag_lifetime
FROM GameConfig
WHERE id = 1;

-- name: GetFlagInsertConfig :one
SELECT COALESCE(flag_lifetime, 0)::INTEGER AS flag_lifetime,
       COALESCE(round_time, 0)::INTEGER AS round_time
FROM GameConfig
WHERE id = 1
FOR SHARE;

-- name: LockOpenCheckerJob :one
SELECT id
FROM CheckerJobs
WHERE id = $1 AND finished_at IS NULL
FOR UPDATE;

-- name: LockSessionForFlag :one
SELECT generation, reset_pending
FROM GameSession
WHERE id = 1
FOR SHARE;

-- name: LockOwnedCheckerJob :one
SELECT job.id
FROM CheckerJobs AS job
JOIN GameSession AS session ON session.id = 1
WHERE job.id = sqlc.arg(job_id)
  AND job.claim_token = sqlc.arg(owner_token)
  AND job.claim_deadline_at > sqlc.arg(claimed_at)
  AND job.deadline_at > sqlc.arg(claimed_at)
  AND job.finished_at IS NULL
  AND NOT session.reset_pending
  AND (job.payload ->> 'generation')::BIGINT = session.generation
FOR UPDATE OF job;

-- name: GetAttackData :many
SELECT t.ip, f.task_id, f.public_flag_data
FROM Flags f
JOIN Teams t ON f.team_id = t.id
WHERE f.round >= sqlc.arg(current_round)::BIGINT - sqlc.arg(flag_lifetime)::BIGINT
  AND f.task_id = ANY(sqlc.arg(task_ids)::UUID[])
ORDER BY f.task_id, t.ip, f.id;

-- name: LockGameForAttack :one
SELECT COALESCE(real_round, 0)::INTEGER AS real_round,
       COALESCE(game_running, FALSE) AS game_running,
       COALESCE(flag_lifetime, 0)::INTEGER AS flag_lifetime,
       COALESCE(game_hardness, 0)::DOUBLE PRECISION AS game_hardness,
       COALESCE(inflation, FALSE) AS inflation,
       COALESCE(volga_attacks_mode, FALSE) AS volga_attacks_mode
FROM GameConfig
WHERE id = 1
FOR SHARE;

-- name: LockSessionForAttack :one
SELECT generation, reset_pending, paused_at, practice_start, final_start
FROM GameSession
WHERE id = 1
FOR SHARE;

-- name: GetSession :one
SELECT id, practice_start, final_start, reset_pending, generation,
       paused_at, paused_seconds, schedule_paused_seconds,
       round_started_at, snapshot_round, snapshot
FROM GameSession
WHERE id = 1;

-- name: PauseSession :execrows
UPDATE GameSession
SET paused_at = sqlc.arg(paused_at)
WHERE id = 1 AND paused_at IS NULL;

-- name: ResumeSession :execrows
UPDATE GameSession
SET paused_seconds = paused_seconds + GREATEST(0, EXTRACT(EPOCH FROM (sqlc.arg(resumed_at)::TIMESTAMPTZ - paused_at))),
    schedule_paused_seconds = schedule_paused_seconds + GREATEST(0, EXTRACT(EPOCH FROM (sqlc.arg(resumed_at)::TIMESTAMPTZ - paused_at))),
    paused_at = NULL
WHERE id = 1 AND paused_at IS NOT NULL;

-- name: LockGameForSession :one
SELECT id
FROM GameConfig
WHERE id = 1
FOR UPDATE;

-- name: LockSessionForUpdate :one
SELECT id
FROM GameSession
WHERE id = 1
FOR UPDATE;

-- name: SaveGameSnapshot :execrows
UPDATE GameSession
SET snapshot_round = sqlc.arg(snapshot_round),
    snapshot = sqlc.arg(snapshot)::JSONB
WHERE id = 1;

-- name: ClaimOutboxJobs :many
WITH pending AS (
    SELECT job.id
    FROM CheckerJobs AS job
    JOIN GameSession AS session ON session.id = 1
    WHERE job.sent_at IS NULL AND job.finished_at IS NULL
      AND (job.publish_token IS NULL OR job.publish_deadline_at <= sqlc.arg(claimed_at))
      AND (job.not_before IS NULL OR job.not_before <= sqlc.arg(claimed_at))
      AND (job.expires_at IS NULL OR job.expires_at > sqlc.arg(claimed_at))
      AND NOT session.reset_pending
      AND session.paused_at IS NULL
      AND (job.payload ->> 'generation')::BIGINT = session.generation
    ORDER BY job.id
    LIMIT sqlc.arg(job_limit)
    FOR UPDATE OF job SKIP LOCKED
)
UPDATE CheckerJobs job
SET publish_token = sqlc.arg(owner_token),
    publish_deadline_at = sqlc.arg(claim_deadline)
FROM pending
WHERE job.id = pending.id
RETURNING job.id, job.run_id, job.round, job.action, job.team_id, job.task_id,
          job.payload, job.sent_at, job.publish_token, job.publish_deadline_at,
          job.claim_token, job.claim_deadline_at, job.put_started_at,
          job.finished_at, job.deadline_at, job.not_before, job.expires_at;

-- name: MarkOutboxPublished :execrows
UPDATE CheckerJobs
SET sent_at = sqlc.arg(published_at),
    publish_token = NULL,
    publish_deadline_at = NULL,
    deadline_at = LEAST(COALESCE(deadline_at, sqlc.arg(initial_deadline)), expires_at)
WHERE CheckerJobs.id = sqlc.arg(job_id)
  AND publish_token = sqlc.arg(owner_token)
  AND publish_deadline_at > sqlc.arg(published_at)
  AND sent_at IS NULL
  AND finished_at IS NULL
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1 AND NOT session.reset_pending
        AND session.paused_at IS NULL
        AND (CheckerJobs.payload ->> 'generation')::BIGINT = session.generation
  );

-- name: ClaimCheckerJob :one
UPDATE CheckerJobs
SET claim_token = sqlc.arg(owner_token),
    claim_deadline_at = LEAST(sqlc.arg(claim_deadline), deadline_at),
    put_started_at = CASE
        WHEN action IN ('classic', 'puts_only') THEN COALESCE(put_started_at, sqlc.arg(claimed_at))
        ELSE put_started_at
    END
WHERE CheckerJobs.id = sqlc.arg(job_id)
  AND sent_at IS NOT NULL
  AND finished_at IS NULL
  AND deadline_at > sqlc.arg(claimed_at)
  AND (claim_token IS NULL OR claim_deadline_at <= sqlc.arg(claimed_at))
  AND action IN ('classic', 'puts_only', 'check_gets', 'check_only')
  AND (action IN ('check_gets', 'check_only') OR put_started_at IS NULL)
  AND (not_before IS NULL OR not_before <= sqlc.arg(claimed_at))
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1 AND NOT session.reset_pending
        AND (CheckerJobs.action IS DISTINCT FROM 'check_only' OR session.paused_at IS NULL)
        AND (CheckerJobs.payload ->> 'generation')::BIGINT = session.generation
  )
RETURNING id, run_id, round, action, team_id, task_id, payload, sent_at,
          publish_token, publish_deadline_at, claim_token, claim_deadline_at,
          put_started_at, finished_at, deadline_at, not_before, expires_at;

-- name: GetCheckerJobState :one
SELECT action AS workflow,
       sent_at, finished_at, deadline_at, claim_token, claim_deadline_at, put_started_at
FROM CheckerJobs
WHERE id = sqlc.arg(job_id)
FOR UPDATE;

-- name: ConfirmCheckerJobOwnership :one
-- A separate connection can resolve an unknown COMMIT without repeating work.
SELECT job.deadline_at, job.claim_deadline_at, job.expires_at
FROM CheckerJobs AS job
JOIN GameSession AS session ON session.id = 1
WHERE job.id = sqlc.arg(job_id)
  AND job.claim_token = sqlc.arg(owner_token)
  AND job.sent_at IS NOT NULL AND job.finished_at IS NULL
  AND job.deadline_at > clock_timestamp()
  AND job.claim_deadline_at > clock_timestamp()
  AND NOT session.reset_pending
  AND (job.payload ->> 'generation')::BIGINT = session.generation;

-- name: ExtendCheckerJobDeadline :execrows
UPDATE CheckerJobs
SET deadline_at = LEAST(GREATEST(deadline_at, sqlc.arg(new_deadline)), expires_at)
WHERE CheckerJobs.id = sqlc.arg(job_id)
  AND claim_token = sqlc.arg(owner_token)
  AND claim_deadline_at > sqlc.arg(touched_at)
  AND deadline_at > sqlc.arg(touched_at)
  AND finished_at IS NULL
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1 AND NOT session.reset_pending
        AND (CheckerJobs.payload ->> 'generation')::BIGINT = session.generation
  );

-- name: SetCheckerJobDeadline :execrows
UPDATE CheckerJobs
SET deadline_at = LEAST(sqlc.arg(new_deadline), expires_at),
    claim_deadline_at = CASE
        WHEN claim_deadline_at IS NULL THEN NULL
        ELSE LEAST(claim_deadline_at, sqlc.arg(new_deadline), expires_at)
    END
WHERE CheckerJobs.id = sqlc.arg(job_id)
  AND claim_token = sqlc.arg(owner_token)
  AND claim_deadline_at > sqlc.arg(touched_at)
  AND deadline_at > sqlc.arg(touched_at)
  AND sqlc.arg(new_deadline) > sqlc.arg(touched_at)
  AND finished_at IS NULL
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1 AND NOT session.reset_pending
        AND (CheckerJobs.payload ->> 'generation')::BIGINT = session.generation
  );

-- name: RenewCheckerJob :execrows
UPDATE CheckerJobs
SET claim_deadline_at = LEAST(sqlc.arg(claim_deadline), deadline_at)
WHERE CheckerJobs.id = sqlc.arg(job_id)
  AND claim_token = sqlc.arg(owner_token)
  AND claim_deadline_at > sqlc.arg(renewed_at)
  AND deadline_at > sqlc.arg(renewed_at)
  AND finished_at IS NULL
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1 AND NOT session.reset_pending
        AND (CheckerJobs.payload ->> 'generation')::BIGINT = session.generation
  );

-- name: CompleteCheckerJobClaim :one
UPDATE CheckerJobs
SET finished_at = sqlc.arg(finished_at),
    result = sqlc.arg(result)::JSONB,
    claim_token = NULL,
    claim_deadline_at = NULL
WHERE CheckerJobs.id = sqlc.arg(job_id)
  AND claim_token = sqlc.arg(owner_token)
  AND claim_deadline_at > sqlc.arg(finished_at)
  AND deadline_at > sqlc.arg(finished_at)
  AND finished_at IS NULL
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1 AND NOT session.reset_pending
        AND (CheckerJobs.payload ->> 'generation')::BIGINT = session.generation
  )
RETURNING CheckerJobs.team_id, CheckerJobs.task_id,
          CheckerJobs.round, CheckerJobs.payload, CheckerJobs.run_id, CheckerJobs.aggregate_round;

-- name: ExpireCheckerJob :one
UPDATE CheckerJobs
SET finished_at = sqlc.arg(finished_at),
    result = sqlc.arg(result)::JSONB,
    claim_token = NULL,
    claim_deadline_at = NULL
WHERE CheckerJobs.id = sqlc.arg(job_id)
  AND finished_at IS NULL
  AND deadline_at <= sqlc.arg(finished_at)
  AND EXISTS (
      SELECT 1 FROM GameSession AS session
      WHERE session.id = 1 AND NOT session.reset_pending
        AND (CheckerJobs.action IS DISTINCT FROM 'check_only' OR session.paused_at IS NULL)
        AND (CheckerJobs.payload ->> 'generation')::BIGINT = session.generation
  )
RETURNING CheckerJobs.team_id, CheckerJobs.task_id,
          CheckerJobs.round, CheckerJobs.payload, CheckerJobs.run_id, CheckerJobs.aggregate_round;

-- name: LogCurrentTeamTask :execrows
INSERT INTO TeamTasksLog
    (round, task_id, team_id, status, stolen, lost, score, checks, checks_passed,
     public_message, private_message, command)
SELECT sqlc.arg(log_round), task.task_id, task.team_id, task.status, task.stolen,
       task.lost, task.score, task.checks, task.checks_passed,
       task.public_message, task.private_message, task.command
FROM TeamTasks AS task
WHERE task.team_id = sqlc.arg(team_id) AND task.task_id = sqlc.arg(task_id)
FOR NO KEY UPDATE;

-- name: UpdateTeamTaskResult :execrows
UPDATE TeamTasks
SET status = sqlc.arg(status),
    public_message = sqlc.arg(public_message),
    private_message = sqlc.arg(private_message),
    command = sqlc.arg(command),
    checks_passed = checks_passed + sqlc.arg(passed_delta),
    checks = checks + sqlc.arg(check_delta)
WHERE TeamTasks.team_id = sqlc.arg(team_id) AND TeamTasks.task_id = sqlc.arg(task_id);

-- name: PendingDispatchRun :one
SELECT id, schedule_id, round, is_round, round_start, snapshot, completed
FROM DispatchRuns
WHERE schedule_id = $1 AND NOT completed;

-- name: HasUnfinishedJobsThroughRound :one
SELECT EXISTS (
    SELECT 1 FROM CheckerJobs
    WHERE round <= $1 AND finished_at IS NULL
) AS has_unfinished;

-- name: FinishDispatchRun :one
UPDATE DispatchRuns AS run
SET completed = TRUE
WHERE run.id = sqlc.arg(run_id)
  AND NOT EXISTS (
      SELECT 1 FROM CheckerJobs AS job
      WHERE job.run_id = run.id
        AND job.sent_at IS NULL AND job.finished_at IS NULL
  )
RETURNING run.schedule_id, run.scheduled_at;

-- name: SaveScheduleCheckpoint :exec
INSERT INTO ScheduleHistory (id, last_run)
VALUES (sqlc.arg(schedule_id), sqlc.arg(last_run))
ON CONFLICT (id) DO UPDATE SET last_run = EXCLUDED.last_run;

-- name: LockGroupedTeamTask :exec
SELECT cell.team_id
FROM TeamTasks AS cell JOIN CheckerJobs AS job
    ON job.team_id = cell.team_id AND job.task_id = cell.task_id
WHERE job.id = sqlc.arg(job_id) AND job.aggregate_round
FOR NO KEY UPDATE OF cell;

-- name: GroupedCheckerResults :many
SELECT finished_at, result
FROM CheckerJobs
WHERE run_id = sqlc.arg(run_id) AND team_id = sqlc.arg(team_id)
  AND task_id = sqlc.arg(task_id) AND aggregate_round
ORDER BY id;

-- name: ResumeExtraChecks :exec
UPDATE CheckerJobs AS job
SET not_before = job.not_before + delay.duration,
    expires_at = job.expires_at + delay.duration,
    deadline_at = job.deadline_at + delay.duration
FROM (
    SELECT GREATEST(INTERVAL '0 seconds', sqlc.arg(resumed_at)::TIMESTAMPTZ - paused_at) AS duration
    FROM GameSession WHERE id = 1 AND paused_at IS NOT NULL
) AS delay
WHERE job.action = 'check_only' AND job.finished_at IS NULL;

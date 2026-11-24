-- name: ReadGameLifecycle :one
SELECT game.id, COALESCE(game.game_running, FALSE)::BOOLEAN AS game_running,
       COALESCE(game.real_round, 0)::INTEGER AS real_round,
       COALESCE(game.flag_lifetime, 0)::INTEGER AS flag_lifetime,
       COALESCE(game.game_hardness, 10)::DOUBLE PRECISION AS game_hardness,
       COALESCE(game.inflation, TRUE)::BOOLEAN AS inflation,
       COALESCE(game.volga_attacks_mode, FALSE)::BOOLEAN AS volga_attacks_mode,
       COALESCE(game.round_time, 60)::INTEGER AS round_time, game.rounds, game.extra_checks,
       COALESCE(game.mode, 'classic')::TEXT AS mode,
       COALESCE(game.timezone, 'UTC')::TEXT AS timezone, game.start_time,
       session.practice_start, session.final_start, session.reset_pending,
       session.generation, session.paused_at, session.paused_seconds,
       session.schedule_paused_seconds, session.round_started_at, session.snapshot_round,
       CASE WHEN sqlc.arg(with_snapshot)::BOOLEAN THEN session.snapshot
           ELSE NULL::JSONB END AS snapshot,
       EXISTS(SELECT 1 FROM CheckerJobs WHERE finished_at IS NULL)::BOOLEAN AS pending
FROM GameConfig AS game
JOIN GameSession AS session ON session.id = game.id
WHERE game.id = 1;

-- name: ListGameTeams :many
SELECT id, name, ip, token, COALESCE(kind,'player')::TEXT AS kind,
       COALESCE(highlighted,FALSE)::BOOLEAN AS highlighted,
       COALESCE(active,FALSE)::BOOLEAN AS active, COALESCE(logo_path,'')::TEXT AS logo_path
FROM Teams WHERE NOT sqlc.arg(active_only)::BOOLEAN OR active ORDER BY id;

-- name: ListGameTasks :many
SELECT id, COALESCE(name,'')::TEXT AS name, COALESCE(checker,'')::TEXT AS checker,
       COALESCE(env_path,'')::TEXT AS env_path, COALESCE(gets,0)::INTEGER AS gets,
       COALESCE(puts,0)::INTEGER AS puts, COALESCE(places,1)::INTEGER AS places,
       COALESCE(checker_timeout,10)::INTEGER AS checker_timeout,
       COALESCE(checker_type,'hackerdom')::TEXT AS checker_type,
       COALESCE(get_period,60)::INTEGER AS get_period,
       COALESCE(default_score,0)::INTEGER AS default_score,
       COALESCE(active,FALSE)::BOOLEAN AS active
FROM Tasks WHERE NOT sqlc.arg(active_only)::BOOLEAN OR active ORDER BY id;

-- name: ListGameCells :many
SELECT tt.team_id,tt.task_id,COALESCE(tt.status,-1)::INTEGER AS status,
       COALESCE(tt.stolen,0)::INTEGER AS stolen,COALESCE(tt.lost,0)::INTEGER AS lost,
       COALESCE(tt.score,0)::DOUBLE PRECISION AS score,
       COALESCE(tt.checks,0)::INTEGER AS checks,COALESCE(tt.checks_passed,0)::INTEGER AS checks_passed,
       tt.public_message,tt.private_message,tt.command
FROM TeamTasks tt JOIN Teams tm ON tm.id=tt.team_id JOIN Tasks ts ON ts.id=tt.task_id
WHERE tm.active AND ts.active ORDER BY tt.team_id,tt.task_id;

-- name: ReadGameHistory :many
SELECT l.id,l.team_id,l.task_id,COALESCE(l.status,-1)::INTEGER AS status,
       COALESCE(l.stolen,0)::INTEGER AS stolen,COALESCE(l.lost,0)::INTEGER AS lost,
       COALESCE(l.score,0)::DOUBLE PRECISION AS score,
       COALESCE(l.checks,0)::INTEGER AS checks,COALESCE(l.checks_passed,0)::INTEGER AS checks_passed,
       l.public_message,l.private_message,l.command,COALESCE(l.round,0)::INTEGER AS round,l.ts
FROM TeamTasksLog l
WHERE l.team_id = sqlc.arg(team_id)::UUID
  AND (sqlc.narg(task_id)::UUID IS NULL OR l.task_id = sqlc.narg(task_id)::UUID)
ORDER BY l.ts DESC, l.id DESC LIMIT 1000;

-- name: ReadPublicCheckerLogs :many
SELECT id, round, action, stage, started_at, finished_at, duration_ms,
       timed_out, status, public_message
FROM CheckerActionLogs
WHERE team_id = sqlc.arg(team_id)::UUID
  AND task_id = sqlc.arg(task_id)::UUID
  AND (sqlc.narg(before_id)::UUID IS NULL OR id < sqlc.narg(before_id)::UUID)
ORDER BY id DESC
LIMIT sqlc.arg(log_limit)::INTEGER;

-- name: InsertPublicCheckerLog :exec
INSERT INTO CheckerActionLogs
    (team_id, task_id, round, action, stage, started_at, finished_at,
     duration_ms, timed_out, status, public_message)
VALUES
    (sqlc.arg(team_id), sqlc.arg(task_id), sqlc.arg(round), sqlc.arg(action),
     sqlc.arg(stage), sqlc.arg(started_at), sqlc.arg(finished_at),
     sqlc.arg(duration_ms), sqlc.arg(timed_out), sqlc.arg(status),
     sqlc.arg(public_message));

-- name: ReadPublicGameHistory :many
WITH lifecycle AS (
    SELECT COALESCE(game.game_running, FALSE) AS game_running,
           COALESCE(game.real_round, 0)::INTEGER AS real_round,
           session.reset_pending, session.snapshot_round, session.snapshot
    FROM GameConfig AS game
    JOIN GameSession AS session ON session.id = game.id
    WHERE game.id = 1
), snapshots AS (
    SELECT snapshot.round, snapshot.round_start, snapshot.snapshot
    FROM GameRoundSnapshots AS snapshot
    WHERE snapshot.round > 0
      AND (
          sqlc.narg(through_round)::INTEGER IS NULL
          OR snapshot.round <= sqlc.narg(through_round)::INTEGER
      )
      AND (
          (
              sqlc.narg(task_id)::UUID IS NULL
              AND sqlc.narg(through_round)::INTEGER IS NULL
          )
          OR EXISTS (
              SELECT 1
              FROM jsonb_array_elements(snapshot.snapshot->'team_tasks') AS candidate(cell)
              WHERE (candidate.cell->>'team_id')::UUID = sqlc.arg(team_id)::UUID
                AND (
                    sqlc.narg(task_id)::UUID IS NULL
                    OR (candidate.cell->>'task_id')::UUID = sqlc.narg(task_id)::UUID
                )
          )
      )
    ORDER BY snapshot.round DESC
    LIMIT 50
), round_windows AS (
    SELECT snapshots.round, snapshots.round_start, snapshots.snapshot,
           current_run.round_start AS receive_start,
           next_run.round_start AS receive_end,
           CASE
               WHEN next_run.round_start IS NULL
                 AND NOT lifecycle.game_running
                 AND NOT lifecycle.reset_pending
                 AND lifecycle.real_round = snapshots.round
                 AND lifecycle.snapshot_round = snapshots.round
                 AND lifecycle.snapshot->>'round' = snapshots.round::TEXT
               THEN TRUE
               ELSE FALSE
           END AS final_round
    FROM snapshots
    CROSS JOIN lifecycle
    LEFT JOIN LATERAL (
        SELECT dispatch.round_start
        FROM DispatchRuns AS dispatch
        WHERE dispatch.is_round
          AND dispatch.round = snapshots.round
          AND dispatch.round_start >= snapshots.round_start
          AND dispatch.round_start < snapshots.round_start + 1
        ORDER BY dispatch.round_start
        LIMIT 1
    ) AS current_run ON TRUE
    LEFT JOIN LATERAL (
        SELECT dispatch.round_start
        FROM DispatchRuns AS dispatch
        WHERE dispatch.is_round
          AND dispatch.round = snapshots.round + 1
          AND dispatch.round_start > current_run.round_start
        ORDER BY dispatch.round_start
        LIMIT 1
    ) AS next_run ON current_run.round_start IS NOT NULL
), history AS (
    SELECT round_windows.round, round_windows.round_start,
           round_windows.receive_start, round_windows.receive_end,
           (
               round_windows.receive_start IS NOT NULL
               AND (
                   round_windows.receive_end IS NOT NULL
                   OR round_windows.final_round
               )
           ) AS receive_window_known,
           cell::JSONB AS cell,
           (cell->>'team_id')::UUID AS team_id,
           (cell->>'task_id')::UUID AS task_id
    FROM round_windows
    CROSS JOIN LATERAL jsonb_array_elements(round_windows.snapshot->'team_tasks') AS cell
    WHERE (cell->>'team_id')::UUID = sqlc.arg(team_id)::UUID
      AND (
          sqlc.narg(task_id)::UUID IS NULL
          OR (cell->>'task_id')::UUID = sqlc.narg(task_id)::UUID
      )
), team_rounds AS (
    SELECT DISTINCT round
    FROM history
), round_events AS (
    SELECT round_windows.round, flag.task_id,
           COUNT(*) FILTER (
               WHERE flag.team_id = sqlc.arg(team_id)::UUID
           ) AS round_flags_lost,
           CASE
               WHEN COUNT(*) FILTER (
                        WHERE flag.team_id = sqlc.arg(team_id)::UUID
                          AND stolen.attacker_delta IS NULL
                    ) > 0
                   THEN NULL::DOUBLE PRECISION
               ELSE COALESCE(
                   SUM(stolen.attacker_delta) FILTER (
                       WHERE flag.team_id = sqlc.arg(team_id)::UUID
                   ),
                   0::DOUBLE PRECISION
               )
           END AS round_opponent_fp,
           COUNT(*) FILTER (
               WHERE stolen.attacker_id = sqlc.arg(team_id)::UUID
                 AND owner.kind IS DISTINCT FROM 'npc'
           ) AS round_flags_captured,
           CASE
               WHEN COUNT(*) FILTER (
                        WHERE stolen.attacker_id = sqlc.arg(team_id)::UUID
                          AND owner.kind IS DISTINCT FROM 'npc'
                          AND stolen.attacker_delta IS NULL
                    ) > 0
                   THEN NULL::DOUBLE PRECISION
               ELSE COALESCE(
                   SUM(stolen.attacker_delta) FILTER (
                       WHERE stolen.attacker_id = sqlc.arg(team_id)::UUID
                         AND owner.kind IS DISTINCT FROM 'npc'
                   ),
                   0::DOUBLE PRECISION
               )
           END AS round_fp_gained
    FROM round_windows
    JOIN team_rounds ON team_rounds.round = round_windows.round
    JOIN StolenFlags AS stolen
      ON stolen.submit_time >= to_timestamp(round_windows.receive_start)
     AND stolen.submit_time < COALESCE(
             to_timestamp(round_windows.receive_end),
             'infinity'::TIMESTAMPTZ
         )
    JOIN Flags AS flag ON flag.id = stolen.flag_id
    JOIN Teams AS owner ON owner.id = flag.team_id
    WHERE (
          flag.team_id = sqlc.arg(team_id)::UUID
          OR (
              stolen.attacker_id = sqlc.arg(team_id)::UUID
              AND owner.kind IS DISTINCT FROM 'npc'
          )
      )
      AND (
          sqlc.narg(task_id)::UUID IS NULL
          OR flag.task_id = sqlc.narg(task_id)::UUID
      )
      AND round_windows.receive_start IS NOT NULL
      AND (
          round_windows.receive_end IS NOT NULL
          OR round_windows.final_round
      )
    GROUP BY round_windows.round, flag.task_id
)
SELECT history.round, history.round_start,
       (
           history.cell || jsonb_build_object(
               'round_flags_lost',
               CASE
                   WHEN history.receive_window_known THEN COALESCE(round_events.round_flags_lost, 0::BIGINT)
                   ELSE NULL::BIGINT
               END,
               'round_flags_captured',
               CASE
                   WHEN history.receive_window_known THEN COALESCE(round_events.round_flags_captured, 0::BIGINT)
                   ELSE NULL::BIGINT
               END,
               'round_opponent_fp',
               CASE
                   WHEN NOT history.receive_window_known THEN NULL::DOUBLE PRECISION
                   WHEN round_events.round_flags_lost IS NULL THEN 0::DOUBLE PRECISION
                   ELSE round_events.round_opponent_fp
               END,
               'round_fp_gained',
               CASE
                   WHEN NOT history.receive_window_known THEN NULL::DOUBLE PRECISION
                   WHEN round_events.round_flags_captured IS NULL THEN 0::DOUBLE PRECISION
                   ELSE round_events.round_fp_gained
               END
           )
       )::JSONB AS cell
FROM history
LEFT JOIN round_events
  ON round_events.round = history.round
 AND round_events.task_id = history.task_id
ORDER BY history.round DESC, history.task_id;

-- name: InsertGameTeam :one
INSERT INTO Teams(name,ip,token,highlighted,active,logo_path,kind)
VALUES(sqlc.arg(name),sqlc.arg(ip),sqlc.arg(token),sqlc.arg(highlighted),
       sqlc.arg(active),sqlc.arg(logo_path),sqlc.arg(kind))
RETURNING id;

-- name: ReplaceGameTeam :one
UPDATE Teams
SET name=sqlc.arg(name),ip=sqlc.arg(ip),token=sqlc.arg(token),
    highlighted=sqlc.arg(highlighted),active=sqlc.arg(active),
    logo_path=sqlc.arg(logo_path),
    kind=COALESCE(NULLIF(sqlc.arg(kind)::TEXT,''),kind)
WHERE id=sqlc.arg(id)
RETURNING kind;

-- name: DisableGameTeam :execrows
UPDATE Teams SET active=FALSE WHERE id=$1;

-- name: InsertTeamCells :exec
INSERT INTO TeamTasks(team_id,task_id,status,score)
SELECT sqlc.arg(team_id),id,-1,default_score FROM Tasks
ON CONFLICT(team_id,task_id) DO NOTHING;

-- name: InsertGameTask :one
INSERT INTO Tasks(name,checker,env_path,gets,puts,places,checker_timeout,checker_type,get_period,default_score,active)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id;

-- name: ReplaceGameTask :execrows
UPDATE Tasks SET name=$2,checker=$3,env_path=$4,gets=$5,puts=$6,places=$7,checker_timeout=$8,
checker_type=$9,get_period=$10,default_score=$11,active=$12 WHERE id=$1;

-- name: DisableGameTask :execrows
UPDATE Tasks SET active=FALSE WHERE id=$1;

-- name: InsertTaskCells :exec
INSERT INTO TeamTasks(team_id,task_id,status,score)
SELECT team.id AS team_id,task.id AS task_id,-1,task.default_score FROM Teams AS team CROSS JOIN Tasks AS task WHERE task.id=sqlc.arg(task_id)
ON CONFLICT(team_id,task_id) DO NOTHING;

-- name: StartGameSession :exec
UPDATE GameSession SET practice_start=$1,final_start=$2,paused_at=NULL,paused_seconds=0,
schedule_paused_seconds=0,generation=generation+1 WHERE id=1;

-- name: SetRunning :exec
UPDATE GameConfig SET game_running=$1 WHERE id=1;

-- name: CancelUnsentJobs :exec
UPDATE CheckerJobs SET finished_at=clock_timestamp()
WHERE sent_at IS NULL AND claim_token IS NULL AND finished_at IS NULL;

-- name: SetGameRoundLimit :execrows
UPDATE GameConfig SET rounds = sqlc.narg(rounds)::INTEGER WHERE id = 1;

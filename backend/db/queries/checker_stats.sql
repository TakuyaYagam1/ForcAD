-- name: ReadCheckerStats :many
WITH actions AS (
    SELECT 'check'::TEXT AS action UNION ALL SELECT 'put' UNION ALL SELECT 'get'
), job_limits AS (
    SELECT job.task_id, a.action, job.finished_at,
           CASE
               WHEN a.action = 'check' AND job.action IN ('classic', 'check_gets', 'check_only') THEN 1
               WHEN a.action = 'put' AND job.action IN ('classic', 'puts_only') THEN (job.payload->'task'->>'puts')::INTEGER
               WHEN a.action = 'get' AND job.action IN ('classic', 'check_gets') THEN (job.payload->'task'->>'gets')::INTEGER
               ELSE 0
           END AS allowance
    FROM CheckerJobs AS job CROSS JOIN actions AS a
    WHERE job.team_id = sqlc.arg(team_id)::UUID AND job.round = sqlc.arg(round)::INTEGER
), planned AS (
    SELECT task_id, action, SUM(allowance) AS maximum,
           SUM(allowance) FILTER (WHERE finished_at IS NOT NULL) AS settled
    FROM job_limits GROUP BY task_id, action
), observed AS (
    SELECT log.task_id, log.action, COUNT(*) AS reported,
           COUNT(*) FILTER (WHERE log.status <> 101) AS failed
    FROM CheckerActionLogs AS log
    JOIN GameSession AS session ON session.id = 1
    JOIN GameConfig AS config ON config.id = session.id
    WHERE log.team_id = sqlc.arg(team_id)::UUID AND log.round = sqlc.arg(round)::INTEGER
      -- Rehearsal cleanup drops dispatch runs but retains action history. A
      -- manual start after the configured start may have no session timestamp.
      AND log.started_at >= GREATEST(
          COALESCE(session.practice_start, session.final_start, config.start_time),
          (SELECT to_timestamp(MIN(run.round_start)) FROM DispatchRuns AS run WHERE run.is_round)
      )
    GROUP BY log.task_id, log.action
)
SELECT task.id AS task_id, COALESCE(task.name, '')::TEXT AS name, a.action::TEXT AS action,
       (CASE WHEN sqlc.arg(round)::INTEGER > 0 THEN COALESCE(p.maximum, 0)
             WHEN a.action = 'put' THEN COALESCE(task.puts, 0)
             WHEN game.mode = 'blitz' AND task.get_period < 0 THEN 0
             WHEN a.action = 'get' THEN COALESCE(task.gets, 0)
             ELSE 1 + game.extra_checks END)::BIGINT AS maximum,
       COALESCE(o.reported, 0)::BIGINT AS reported, COALESCE(o.failed, 0)::BIGINT AS failed,
       GREATEST(0, COALESCE(p.settled, 0) - COALESCE(o.reported, 0))::BIGINT AS no_result
FROM Tasks AS task CROSS JOIN actions AS a
JOIN GameConfig AS game ON game.id = 1
JOIN Teams AS team ON team.id = sqlc.arg(team_id)::UUID AND team.active
LEFT JOIN planned AS p ON p.task_id = task.id AND p.action = a.action AND sqlc.arg(round)::INTEGER > 0
LEFT JOIN observed AS o ON o.task_id = task.id AND o.action = a.action AND sqlc.arg(round)::INTEGER > 0
WHERE task.active
ORDER BY task.id, a.action;

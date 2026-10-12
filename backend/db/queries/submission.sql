-- name: SubmissionRound :one
SELECT COALESCE(
    (SELECT round FROM DispatchRuns
     WHERE is_round AND round_start <= sqlc.arg(received_at)::DOUBLE PRECISION
     ORDER BY round_start DESC LIMIT 1),
    CASE WHEN EXISTS (SELECT 1 FROM DispatchRuns WHERE is_round)
        THEN 0 ELSE sqlc.arg(current_round)::INTEGER END
)::INTEGER AS round;

-- name: ReadSubmissionFlags :many
SELECT f.id, f.flag, f.team_id, f.task_id, f.round,
       EXISTS (SELECT 1 FROM StolenFlags s
               WHERE s.flag_id = f.id AND s.attacker_id = sqlc.arg(attacker_id)) AS stolen
FROM Flags f
WHERE f.flag = ANY(sqlc.arg(flag_values)::VARCHAR[])
ORDER BY f.id;

-- name: LockSubmissionTasks :many
WITH wanted AS (
    SELECT UNNEST(sqlc.arg(team_ids)::UUID[]) AS team_id,
           UNNEST(sqlc.arg(task_ids)::UUID[]) AS task_id
)
SELECT t.team_id, t.task_id, COALESCE(t.status, -1)::INTEGER AS status,
       COALESCE(t.score, 0)::DOUBLE PRECISION AS score
FROM TeamTasks t
JOIN wanted
    ON wanted.team_id = t.team_id AND wanted.task_id = t.task_id
ORDER BY t.team_id, t.task_id
FOR NO KEY UPDATE OF t;

-- name: InsertStolenFlags :many
INSERT INTO StolenFlags (attacker_id, flag_id, submit_time)
SELECT sqlc.arg(attacker_id), id, sqlc.arg(received_at)::TIMESTAMPTZ
FROM UNNEST(sqlc.arg(flag_ids)::UUID[]) AS flags(id)
ORDER BY id
ON CONFLICT (flag_id, attacker_id) DO NOTHING
RETURNING flag_id;

-- name: UpdateSubmissionScores :execrows
WITH changes AS (
    SELECT UNNEST(sqlc.arg(team_ids)::UUID[]) AS team_id,
           UNNEST(sqlc.arg(task_ids)::UUID[]) AS task_id,
           UNNEST(sqlc.arg(stolen)::INTEGER[]) AS stolen,
           UNNEST(sqlc.arg(lost)::INTEGER[]) AS lost,
           UNNEST(sqlc.arg(scores)::DOUBLE PRECISION[]) AS score
)
UPDATE TeamTasks t
SET stolen = t.stolen + changes.stolen,
    lost = t.lost + changes.lost,
    score = changes.score
FROM changes
WHERE t.team_id = changes.team_id AND t.task_id = changes.task_id;

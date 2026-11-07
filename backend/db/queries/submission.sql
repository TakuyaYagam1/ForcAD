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

-- name: ApplySubmissionChanges :one
WITH score_changes AS (
    SELECT UNNEST(sqlc.arg(team_ids)::UUID[]) AS team_id,
           UNNEST(sqlc.arg(task_ids)::UUID[]) AS task_id,
           UNNEST(sqlc.arg(stolen)::INTEGER[]) AS stolen,
           UNNEST(sqlc.arg(lost)::INTEGER[]) AS lost,
           UNNEST(sqlc.arg(scores)::DOUBLE PRECISION[]) AS score
), updated_scores AS (
    UPDATE TeamTasks AS task
    SET stolen = task.stolen + score_changes.stolen,
        lost = task.lost + score_changes.lost,
        score = score_changes.score
    FROM score_changes
    WHERE task.team_id = score_changes.team_id AND task.task_id = score_changes.task_id
    RETURNING task.team_id
), event_changes AS (
    SELECT flags.flag_id, deltas.attacker_delta
    FROM UNNEST(sqlc.arg(flag_ids)::UUID[]) WITH ORDINALITY AS flags(flag_id, ordinal)
    JOIN UNNEST(sqlc.arg(deltas)::DOUBLE PRECISION[]) WITH ORDINALITY AS deltas(attacker_delta, ordinal)
        USING (ordinal)
), updated_events AS (
    UPDATE StolenFlags AS event
    SET attacker_delta = event_changes.attacker_delta
    FROM event_changes
    WHERE event.flag_id = event_changes.flag_id
      AND event.attacker_id = sqlc.arg(attacker_id)::UUID
    RETURNING event.flag_id
)
SELECT (SELECT count(*) FROM updated_scores) AS score_rows,
       (SELECT count(*) FROM updated_events) AS event_rows;

-- name: InsertSubmissionChanges :one
WITH flag_changes AS (
    SELECT UNNEST(sqlc.arg(flag_ids)::UUID[]) AS flag_id,
           UNNEST(sqlc.arg(deltas)::DOUBLE PRECISION[]) AS attacker_delta
), inserted AS (
    INSERT INTO StolenFlags (attacker_id, flag_id, submit_time, attacker_delta)
    SELECT sqlc.arg(attacker_id)::UUID, flag_id,
           sqlc.arg(received_at)::TIMESTAMPTZ, attacker_delta
    FROM flag_changes
    ORDER BY flag_id
    ON CONFLICT (flag_id, attacker_id) DO NOTHING
    RETURNING flag_id
), score_changes AS (
    SELECT UNNEST(sqlc.arg(team_ids)::UUID[]) AS team_id,
           UNNEST(sqlc.arg(task_ids)::UUID[]) AS task_id,
           UNNEST(sqlc.arg(stolen)::INTEGER[]) AS stolen,
           UNNEST(sqlc.arg(lost)::INTEGER[]) AS lost,
           UNNEST(sqlc.arg(scores)::DOUBLE PRECISION[]) AS score
), updated_scores AS (
    UPDATE TeamTasks AS task
    SET stolen = task.stolen + score_changes.stolen,
        lost = task.lost + score_changes.lost,
        score = score_changes.score
    FROM score_changes
    WHERE task.team_id = score_changes.team_id AND task.task_id = score_changes.task_id
      AND (SELECT count(*) FROM inserted) = cardinality(sqlc.arg(flag_ids)::UUID[])
    RETURNING task.team_id
)
SELECT ARRAY(SELECT flag_id FROM inserted)::UUID[] AS flag_ids,
       (SELECT count(*) FROM updated_scores) AS score_rows;

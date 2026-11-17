-- name: CountExistingTeams :one
SELECT count(*)::INTEGER
FROM (
    SELECT id
    FROM Teams
    WHERE id = ANY(sqlc.arg(team_ids)::UUID[])
    FOR UPDATE
) AS existing_teams;

-- name: DeactivateTeams :execrows
UPDATE Teams
SET active = FALSE
WHERE active AND id = ANY(sqlc.arg(team_ids)::UUID[]);

-- name: ActivateTeams :execrows
UPDATE Teams
SET active = TRUE
WHERE NOT active AND id = ANY(sqlc.arg(team_ids)::UUID[]);

-- name: CountExistingTasks :one
SELECT count(*)::INTEGER
FROM (
    SELECT id
    FROM Tasks
    WHERE id = ANY(sqlc.arg(task_ids)::UUID[])
    FOR UPDATE
) AS existing_tasks;

-- name: DeactivateTasks :execrows
UPDATE Tasks
SET active = FALSE
WHERE active AND id = ANY(sqlc.arg(task_ids)::UUID[]);

-- name: ActivateTasks :execrows
UPDATE Tasks
SET active = TRUE
WHERE NOT active AND id = ANY(sqlc.arg(task_ids)::UUID[]);

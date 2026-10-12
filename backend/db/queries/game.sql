-- name: ReadGameLifecycle :one
SELECT game.id, COALESCE(game.game_running, FALSE)::BOOLEAN AS game_running,
       COALESCE(game.real_round, 0)::INTEGER AS real_round,
       COALESCE(game.flag_lifetime, 0)::INTEGER AS flag_lifetime,
       COALESCE(game.game_hardness, 10)::DOUBLE PRECISION AS game_hardness,
       COALESCE(game.inflation, TRUE)::BOOLEAN AS inflation,
       COALESCE(game.volga_attacks_mode, FALSE)::BOOLEAN AS volga_attacks_mode,
       COALESCE(game.round_time, 60)::INTEGER AS round_time, game.rounds,
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
SELECT id, name, ip, token, COALESCE(highlighted,FALSE)::BOOLEAN AS highlighted,
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

-- name: ReadPublicGameHistory :many
SELECT snapshots.round, snapshots.round_start, cell::JSONB AS cell
FROM (
    SELECT round, round_start, snapshot FROM GameRoundSnapshots
    ORDER BY round DESC LIMIT 50
) snapshots
CROSS JOIN LATERAL jsonb_array_elements(snapshots.snapshot->'team_tasks') AS cell
WHERE (cell->>'team_id')::UUID = sqlc.arg(team_id)::UUID
ORDER BY snapshots.round DESC, (cell->>'task_id')::UUID;

-- name: InsertGameTeam :one
INSERT INTO Teams(name,ip,token,highlighted,active,logo_path) VALUES($1,$2,$3,$4,$5,$6) RETURNING id;

-- name: ReplaceGameTeam :execrows
UPDATE Teams SET name=$2,ip=$3,token=$4,highlighted=$5,active=$6,logo_path=$7 WHERE id=$1;

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

-- name: LockInitialization :exec
SELECT pg_advisory_xact_lock(718221946);

-- name: GameIsInitialized :one
SELECT EXISTS(SELECT 1 FROM GameConfig)::BOOLEAN;

-- name: InitializeGame :exec
INSERT INTO GameConfig(id,game_running,real_round,flag_lifetime,game_hardness,inflation,
                      volga_attacks_mode,round_time,rounds,mode,timezone,start_time,extra_checks)
VALUES(1,FALSE,0,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10);

-- name: InitializeGameCells :exec
INSERT INTO TeamTasks(team_id,task_id,status,score)
SELECT team.id AS team_id,task.id AS task_id,-1,task.default_score FROM Teams AS team CROSS JOIN Tasks AS task
ON CONFLICT(team_id,task_id) DO NOTHING;

-- name: ResetGameTables :exec
TRUNCATE TABLE GameRoundSnapshots, CheckerJobs, DispatchRuns, PublicAnnouncements, StolenFlags, Flags,
    TeamTasksLog, TeamTasks, ScheduleHistory, GameSession, GameConfig, Tasks, Teams;

-- name: InitializeEmptySession :exec
INSERT INTO GameSession(id) VALUES (1);

-- name: UpdateGameSettings :execrows
UPDATE GameConfig
SET flag_lifetime = sqlc.arg(flag_lifetime)::INTEGER,
    game_hardness = sqlc.arg(game_hardness)::DOUBLE PRECISION,
    inflation = sqlc.arg(inflation)::BOOLEAN,
    volga_attacks_mode = sqlc.arg(volga_attacks_mode)::BOOLEAN,
    round_time = sqlc.arg(round_time)::INTEGER,
    rounds = sqlc.narg(rounds)::INTEGER,
    extra_checks = sqlc.arg(extra_checks)::INTEGER,
    mode = sqlc.arg(mode)::TEXT,
    timezone = sqlc.arg(timezone)::TEXT,
    start_time = sqlc.arg(start_time)::TIMESTAMP WITH TIME ZONE
WHERE id = 1;

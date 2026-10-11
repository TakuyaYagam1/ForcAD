-- Runtime sessions keep rehearsal state separate from the official schedule.
CREATE TABLE IF NOT EXISTS GameSession
(
    id INTEGER PRIMARY KEY CHECK (id = 1),
    practice_start TIMESTAMP WITH TIME ZONE,
    final_start TIMESTAMP WITH TIME ZONE,
    reset_pending BOOLEAN NOT NULL DEFAULT FALSE,
    generation BIGINT NOT NULL DEFAULT 0
);

ALTER TABLE GameSession
    ADD COLUMN IF NOT EXISTS final_start TIMESTAMP WITH TIME ZONE;

INSERT INTO GameSession (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

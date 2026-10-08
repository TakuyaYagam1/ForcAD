CREATE TABLE IF NOT EXISTS DispatchRuns
(
    id TEXT PRIMARY KEY,
    schedule_id TEXT NOT NULL,
    round INTEGER NOT NULL,
    is_round BOOLEAN NOT NULL,
    round_start DOUBLE PRECISION NOT NULL,
    snapshot JSONB,
    completed BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE UNIQUE INDEX IF NOT EXISTS dispatch_pending_schedule
    ON DispatchRuns(schedule_id) WHERE NOT completed;

CREATE TABLE IF NOT EXISTS CheckerJobs
(
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES DispatchRuns(id) ON DELETE CASCADE,
    round INTEGER NOT NULL,
    payload JSONB NOT NULL,
    sent_at TIMESTAMP WITH TIME ZONE,
    finished_at TIMESTAMP WITH TIME ZONE,
    deadline_at TIMESTAMP WITH TIME ZONE
);

ALTER TABLE CheckerJobs
    ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMP WITH TIME ZONE;

-- Existing in-flight jobs receive a full recovery grace period on migration.
UPDATE CheckerJobs
SET deadline_at = clock_timestamp() + make_interval(
    secs => (payload -> 'task' ->> 'checker_timeout')::INTEGER + 300
)
WHERE sent_at IS NOT NULL AND finished_at IS NULL AND deadline_at IS NULL;

CREATE INDEX IF NOT EXISTS checker_jobs_pending
    ON CheckerJobs(round) WHERE finished_at IS NULL;

CREATE INDEX IF NOT EXISTS checker_jobs_run
    ON CheckerJobs(run_id);

CREATE INDEX IF NOT EXISTS checker_jobs_deadline
    ON CheckerJobs(deadline_at) WHERE finished_at IS NULL;

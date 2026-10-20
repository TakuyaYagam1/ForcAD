-- +goose Up

-- Refuse to merge migrations into an existing unversioned schema.
-- +goose StatementBegin
DO $$
DECLARE
    schema_oid oid := current_schema()::regnamespace;
    goose_version_table oid;
BEGIN
    SELECT relation.oid INTO goose_version_table
    FROM pg_class AS relation
    WHERE relation.relnamespace = schema_oid
      AND relation.relname = 'goose_db_version'
      AND relation.relkind = 'r';

    IF EXISTS (
        SELECT 1
        FROM pg_class AS relation
        WHERE relation.relnamespace = schema_oid
          AND relation.relkind IN ('r', 'p', 'v', 'm', 'f', 'S', 'c')
          AND relation.oid IS DISTINCT FROM goose_version_table
          AND NOT (
              relation.relkind = 'S'
              AND EXISTS (
                  SELECT 1
                  FROM pg_depend AS ownership
                  WHERE ownership.classid = 'pg_class'::regclass
                    AND ownership.objid = relation.oid
                    AND ownership.refclassid = 'pg_class'::regclass
                    AND ownership.refobjid = goose_version_table
                    AND ownership.deptype IN ('a', 'i')
              )
          )
    ) OR EXISTS (
        SELECT 1 FROM pg_proc AS routine
        WHERE routine.pronamespace = schema_oid
    ) OR EXISTS (
        SELECT 1 FROM pg_type AS type
        WHERE type.typnamespace = schema_oid
          AND type.typrelid = 0
          AND type.typelem = 0
          AND type.typtype IN ('b', 'd', 'e', 'r', 'm')
    ) THEN
        RAISE EXCEPTION
            'refusing to migrate nonempty unversioned schema: user objects already exist in schema %',
            current_schema();
    END IF;
END;
$$;
-- +goose StatementEnd

CREATE TABLE GameConfig
(
    id                 INTEGER PRIMARY KEY CHECK (id = 1),
    game_running       BOOLEAN DEFAULT FALSE,
    real_round         INTEGER DEFAULT 0,
    flag_lifetime      INTEGER CHECK (flag_lifetime > 0),
    game_hardness      FLOAT CHECK (game_hardness > 1 AND game_hardness < 'Infinity'::float8),
    inflation          BOOLEAN,
    volga_attacks_mode BOOLEAN,
    round_time         INTEGER CHECK (round_time > 0),
    rounds             INTEGER CHECK (rounds > 0),
    mode               VARCHAR(8) DEFAULT 'classic',
    timezone           VARCHAR(32) DEFAULT 'UTC',
    start_time         TIMESTAMP WITH TIME ZONE,
    extra_checks       INTEGER NOT NULL DEFAULT 0 CHECK (extra_checks BETWEEN 0 AND 8),
    CONSTRAINT extra_checks_classic CHECK (extra_checks = 0 OR mode IS NOT DISTINCT FROM 'classic')
);

CREATE TABLE Teams
(
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    name        VARCHAR(255) NOT NULL DEFAULT '',
    ip          VARCHAR(45) NOT NULL,
    token       VARCHAR(16) NOT NULL,
    highlighted BOOLEAN DEFAULT FALSE,
    active      BOOLEAN DEFAULT TRUE,
    logo_path   VARCHAR(255) DEFAULT '',
    CONSTRAINT teams_token_format_check CHECK (token ~ '^[0-9a-f]{16}$'),
    CONSTRAINT teams_token_unique UNIQUE (token)
);

CREATE TABLE Tasks
(
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    name            VARCHAR(255),
    checker         VARCHAR(1024),
    env_path        VARCHAR(1024),
    gets            INTEGER CHECK (gets >= 0),
    puts            INTEGER CHECK (puts >= 0),
    places          INTEGER CHECK (places > 0),
    checker_timeout INTEGER CHECK (checker_timeout > 0),
    checker_type    VARCHAR(32) DEFAULT 'hackerdom',
    get_period      INTEGER DEFAULT 0,
    default_score   INTEGER CHECK (default_score >= 0),
    active          BOOLEAN DEFAULT TRUE
);

CREATE TABLE Flags
(
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    flag              VARCHAR(32) UNIQUE NOT NULL DEFAULT '',
    team_id           UUID NOT NULL REFERENCES Teams(id) ON DELETE RESTRICT,
    task_id           UUID NOT NULL REFERENCES Tasks(id) ON DELETE RESTRICT,
    round             INTEGER NOT NULL,
    public_flag_data  TEXT NOT NULL,
    private_flag_data TEXT NOT NULL,
    vuln_number       INTEGER
);

CREATE TABLE StolenFlags
(
    flag_id     UUID NOT NULL REFERENCES Flags(id) ON DELETE RESTRICT,
    attacker_id UUID NOT NULL REFERENCES Teams(id) ON DELETE RESTRICT,
    submit_time TIMESTAMP WITH TIME ZONE DEFAULT now(),
    PRIMARY KEY (flag_id, attacker_id)
);

CREATE TABLE TeamTasks
(
    task_id         UUID NOT NULL REFERENCES Tasks(id) ON DELETE CASCADE,
    team_id         UUID NOT NULL REFERENCES Teams(id) ON DELETE CASCADE,
    status          INTEGER,
    stolen          INTEGER DEFAULT 0 CHECK (stolen >= 0),
    lost            INTEGER DEFAULT 0 CHECK (lost >= 0),
    score           FLOAT DEFAULT 0 CHECK (score >= 0),
    checks          INTEGER DEFAULT 0,
    checks_passed   INTEGER DEFAULT 0,
    public_message  TEXT NOT NULL DEFAULT '',
    private_message TEXT NOT NULL DEFAULT '',
    command         TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (team_id, task_id),
    CONSTRAINT sla_valid CHECK (checks >= 0 AND checks_passed >= 0 AND checks_passed <= checks)
);

CREATE TABLE TeamTasksLog
(
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    round           INTEGER,
    task_id         UUID,
    team_id         UUID,
    status          INTEGER,
    stolen          INTEGER DEFAULT 0,
    lost            INTEGER DEFAULT 0,
    score           FLOAT DEFAULT 0,
    checks          INTEGER DEFAULT 0,
    checks_passed   INTEGER DEFAULT 0,
    public_message  TEXT NOT NULL DEFAULT '',
    private_message TEXT NOT NULL DEFAULT '',
    command         TEXT NOT NULL DEFAULT '',
    ts              TIMESTAMP WITH TIME ZONE DEFAULT now()
);

CREATE TABLE ScheduleHistory
(
    id       TEXT PRIMARY KEY,
    last_run TIMESTAMP WITH TIME ZONE DEFAULT now()
);

CREATE TABLE GameSession
(
    id                      INTEGER PRIMARY KEY CHECK (id = 1),
    practice_start          TIMESTAMP WITH TIME ZONE,
    final_start             TIMESTAMP WITH TIME ZONE,
    reset_pending           BOOLEAN NOT NULL DEFAULT FALSE,
    generation              BIGINT NOT NULL DEFAULT 0,
    paused_at               TIMESTAMP WITH TIME ZONE,
    paused_seconds          DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (paused_seconds >= 0),
    schedule_paused_seconds DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (schedule_paused_seconds >= 0),
    round_started_at        TIMESTAMP WITH TIME ZONE,
    snapshot_round          INTEGER,
    snapshot                JSONB
);

CREATE TABLE DispatchRuns
(
    id                   UUID PRIMARY KEY DEFAULT uuidv7(),
    schedule_id          TEXT NOT NULL,
    round                INTEGER NOT NULL,
    is_round             BOOLEAN NOT NULL,
    round_start          DOUBLE PRECISION NOT NULL,
    snapshot             JSONB,
    completed            BOOLEAN NOT NULL DEFAULT FALSE,
    snapshot_round       INTEGER,
    snapshot_round_start DOUBLE PRECISION,
    scheduled_at         TIMESTAMP WITH TIME ZONE
);

CREATE TABLE CheckerJobs
(
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    run_id              UUID NOT NULL REFERENCES DispatchRuns(id) ON DELETE CASCADE,
    round               INTEGER NOT NULL,
    payload             JSONB NOT NULL,
    action              VARCHAR(16) CHECK (action IS NULL OR action IN ('classic', 'puts_only', 'check_gets', 'check_only')),
    team_id             UUID,
    task_id             UUID,
    sent_at             TIMESTAMP WITH TIME ZONE,
    publish_token       TEXT,
    publish_deadline_at TIMESTAMP WITH TIME ZONE,
    claim_token         TEXT,
    claim_deadline_at   TIMESTAMP WITH TIME ZONE,
    put_started_at      TIMESTAMP WITH TIME ZONE,
    finished_at         TIMESTAMP WITH TIME ZONE,
    deadline_at         TIMESTAMP WITH TIME ZONE,
    not_before          TIMESTAMP WITH TIME ZONE,
    expires_at          TIMESTAMP WITH TIME ZONE,
    aggregate_round     BOOLEAN NOT NULL DEFAULT FALSE,
    result              JSONB
);

CREATE TABLE GameRoundSnapshots
(
    round       INTEGER PRIMARY KEY CHECK (round > 0),
    round_start DOUBLE PRECISION NOT NULL CHECK (round_start >= 0),
    snapshot    JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT clock_timestamp()
);

INSERT INTO GameSession (id) VALUES (1);

CREATE INDEX idx_flags_round_team ON Flags (round, team_id);
CREATE INDEX idx_flags_round_task ON Flags (round, task_id);
CREATE INDEX dispatch_round_start ON DispatchRuns (round_start DESC) INCLUDE (round) WHERE is_round;
CREATE INDEX idx_teamtaskslog_history ON TeamTasksLog (team_id, ts DESC, id DESC);
CREATE UNIQUE INDEX dispatch_pending_schedule
    ON DispatchRuns (schedule_id) WHERE NOT completed;
CREATE INDEX checker_jobs_pending
    ON CheckerJobs (round) WHERE finished_at IS NULL;
CREATE INDEX checker_jobs_run ON CheckerJobs (run_id);
CREATE INDEX checker_jobs_deadline
    ON CheckerJobs (deadline_at) WHERE finished_at IS NULL;
CREATE INDEX checker_jobs_publish_deadline
    ON CheckerJobs (publish_deadline_at) WHERE sent_at IS NULL;
CREATE INDEX checker_jobs_claim_deadline
    ON CheckerJobs (claim_deadline_at) WHERE finished_at IS NULL;
CREATE INDEX checker_jobs_round_group
    ON CheckerJobs (run_id, team_id, task_id) WHERE aggregate_round;
CREATE INDEX checker_jobs_not_before
    ON CheckerJobs (not_before, id) WHERE sent_at IS NULL AND finished_at IS NULL;
CREATE INDEX game_round_snapshots_created
    ON GameRoundSnapshots (created_at, round);

-- +goose Down

DROP TABLE GameRoundSnapshots;
DROP TABLE CheckerJobs;
DROP TABLE DispatchRuns;
DROP TABLE GameSession;
DROP TABLE ScheduleHistory;
DROP TABLE TeamTasksLog;
DROP TABLE TeamTasks;
DROP TABLE StolenFlags;
DROP TABLE Flags;
DROP TABLE Tasks;
DROP TABLE Teams;
DROP TABLE GameConfig;

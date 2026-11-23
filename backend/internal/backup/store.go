package backup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type table struct {
	db, archive, columns string
}

// The order ensures referenced rows exist before COPY checks foreign keys.
var tables = [...]table{
	{"gameconfig", "game_config", "id, game_running, real_round, flag_lifetime, game_hardness, inflation, volga_attacks_mode, round_time, rounds, mode, timezone, start_time, extra_checks"},
	{"gamesession", "game_session", "id, practice_start, final_start, reset_pending, generation, paused_at, paused_seconds, schedule_paused_seconds, round_started_at, snapshot_round, snapshot"},
	{"teams", "teams", "id, name, ip, token, highlighted, active, logo_path, kind"},
	{"tasks", "tasks", "id, name, checker, env_path, gets, puts, places, checker_timeout, checker_type, get_period, default_score, active"},
	{"flags", "flags", "id, flag, team_id, task_id, round, public_flag_data, private_flag_data, vuln_number"},
	{"stolenflags", "stolen_flags", "flag_id, attacker_id, submit_time, attacker_delta"},
	{"teamtasks", "team_tasks", "task_id, team_id, status, stolen, lost, score, checks, checks_passed, public_message, private_message, command"},
	{"teamtaskslog", "team_tasks_log", "id, round, task_id, team_id, status, stolen, lost, score, checks, checks_passed, public_message, private_message, command, ts"},
	{"schedulehistory", "schedule_history", "id, last_run"},
	{"dispatchruns", "dispatch_runs", "id, schedule_id, round, is_round, round_start, snapshot, completed, snapshot_round, snapshot_round_start, scheduled_at"},
	{"checkerjobs", "checker_jobs", "id, run_id, round, payload, action, team_id, task_id, sent_at, publish_token, publish_deadline_at, claim_token, claim_deadline_at, put_started_at, finished_at, deadline_at, not_before, expires_at, aggregate_round, result"},
	{"gameroundsnapshots", "game_round_snapshots", "round, round_start, snapshot, created_at"},
	{"checkeractionlogs", "checker_action_logs", "id, team_id, task_id, round, action, stage, started_at, finished_at, duration_ms, timed_out, status, public_message"},
	{"publicannouncements", "public_announcements", "id, title, markdown, author, created_at"},
}

func (t table) member() string {
	return "data/" + t.archive + ".ndjson"
}

type Store struct {
	pool    *pgxpool.Pool
	logoDir string
	slot    chan struct{}
}

func New(pool *pgxpool.Pool, logoDir string) *Store {
	return &Store{
		pool:    pool,
		logoDir: logoDir,
		slot:    make(chan struct{}, 1),
	}
}

func (s *Store) takeSlot() bool {
	select {
	case s.slot <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Store) Export(ctx context.Context, out io.Writer, name string) (Manifest, error) {
	const location = "backup.Store.Export"
	if !ValidName(name) {
		return Manifest{}, fmt.Errorf("%s - invalid event name: %w", location, ErrInvalid)
	}
	if !s.takeSlot() {
		return Manifest{}, ErrBusy
	}
	defer func() {
		<-s.slot
	}()
	b, err := newBundle()
	if err != nil {
		return Manifest{}, err
	}
	defer b.close()
	b.manifest.Version = formatVersion
	b.manifest.ForcAD = buildVersion()
	b.manifest.Name = name
	err = pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL TIME ZONE 'UTC'; SET LOCAL statement_timeout = '10min'"); err != nil {
			return err
		}
		// The first SELECT takes the MVCC snapshot. Record its database time,
		// not when the archive started staging or finished compressing.
		const query = `SELECT g.real_round, statement_timestamp()
FROM gameconfig g JOIN gamesession s USING (id) WHERE g.id = 1 AND g.real_round >= 0`
		if err := tx.QueryRow(ctx, query).Scan(&b.manifest.Round, &b.manifest.CreatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSource
			}
			return err
		}
		b.manifest.CreatedAt = b.manifest.CreatedAt.UTC()
		for _, table := range tables {
			if err := b.add(table.member(), func(w io.Writer) error {
				return exportTable(ctx, tx, table, w)
			}); err != nil {
				return err
			}
		}
		return s.exportLogos(ctx, tx, b)
	})
	if err != nil {
		return Manifest{}, fmt.Errorf("%s - export event data: %w", location, err)
	}
	if err := b.writeArchive(ctx, out); err != nil {
		return Manifest{}, err
	}
	return b.manifest, nil
}

func (s *Store) Inspect(ctx context.Context, input io.Reader) (Manifest, error) {
	if !s.takeSlot() {
		return Manifest{}, ErrBusy
	}
	defer func() {
		<-s.slot
	}()
	b, err := readArchive(ctx, input)
	if err != nil {
		return Manifest{}, err
	}
	defer b.close()
	return b.manifest, nil
}

func (s *Store) Restore(ctx context.Context, input io.Reader) (Manifest, error) {
	const location = "backup.Store.Restore"
	if !s.takeSlot() {
		return Manifest{}, ErrBusy
	}
	defer func() {
		<-s.slot
	}()
	b, err := readArchive(ctx, input)
	if err != nil {
		return Manifest{}, err
	}
	defer b.close()
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return restoreBundle(ctx, tx, b)
	})
	if err != nil {
		return Manifest{}, fmt.Errorf("%s - restore event: %w", location, err)
	}
	return b.manifest, nil
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
		if info.Main.Version != "" {
			return info.Main.Version
		}
	}
	return "development"
}

func exportTable(
	ctx context.Context,
	tx pgx.Tx,
	table table,
	out io.Writer,
) error {
	const location = "backup.exportTable"
	rows, err := tx.Query(ctx, "SELECT row_to_json(t) FROM (SELECT "+table.columns+" FROM "+pgx.Identifier{table.db}.Sanitize()+") AS t")
	if err != nil {
		return err
	}
	defer rows.Close()
	writer := bufio.NewWriter(out)
	for rows.Next() {
		// The JSON datum is borrowed from pgx until the next Next call.
		data := rows.RawValues()[0]
		if len(data) > maxRowBytes {
			return fmt.Errorf("%s - oversized row in %s: %w", location, table.db, ErrInvalid)
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
	}
	return errors.Join(rows.Err(), writer.Flush())
}

func restoreBundle(ctx context.Context, tx pgx.Tx, b *bundle) error {
	const location = "backup.restoreBundle"
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '10min'"); err != nil {
		return fmt.Errorf("%s - configure restore deadline: %w", location, err)
	}
	const guard = `LOCK TABLE gameconfig, gamesession, teams, tasks, flags, stolenflags, teamtasks,
teamtaskslog, schedulehistory, dispatchruns, checkerjobs, gameroundsnapshots, checkeractionlogs,
publicannouncements, backup_team_logos IN SHARE ROW EXCLUSIVE MODE NOWAIT`
	if _, err := tx.Exec(ctx, guard); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			return ErrBusy
		}
		return fmt.Errorf("%s - lock destination game: %w", location, err)
	}
	generation, err := cleanGeneration(ctx, tx)
	if err != nil {
		return err
	}
	for i := len(tables) - 1; i >= 0; i-- {
		if _, err := tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{tables[i].db}.Sanitize()); err != nil {
			return fmt.Errorf("%s - clear target game seed: %w", location, err)
		}
	}
	for _, table := range tables {
		if err := importTable(ctx, tx, table, b.files[table.member()]); err != nil {
			return fmt.Errorf("%s - restore %s: %w: %w", location, table.archive, ErrInvalid, err)
		}
	}
	if err := importLogos(ctx, tx, b); err != nil {
		return err
	}
	return prepareRestoredSession(ctx, tx, b.manifest, generation)
}

func prepareRestoredSession(
	ctx context.Context,
	tx pgx.Tx,
	m Manifest,
	targetGeneration int64,
) error {
	const location = "backup.prepareRestoredSession"
	// Preserve existing pauses. ResumeSession uses paused_at to exclude
	// downtime from both the current round and the scheduler clock.
	const session = `UPDATE gamesession s SET generation = GREATEST(s.generation, $1) + 1,
paused_at = CASE WHEN g.game_running THEN COALESCE(s.paused_at, $2) ELSE s.paused_at END
FROM gameconfig g WHERE s.id = 1 AND g.id = s.id AND g.real_round = $3
AND NOT EXISTS (SELECT 1 FROM checkerjobs j WHERE j.finished_at IS NULL
    AND (j.payload->'generation' IS DISTINCT FROM to_jsonb(s.generation)
         OR (j.sent_at IS NULL AND (j.claim_token IS NOT NULL OR j.put_started_at IS NOT NULL))))`
	tag, err := tx.Exec(ctx, session, targetGeneration, m.CreatedAt, m.Round)
	if err != nil {
		return fmt.Errorf("%s - fence restored session: %w", location, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%s - inconsistent archive round, session or unfinished jobs: %w", location, ErrInvalid)
	}

	// The broker and worker leases aren't portable. Unsent work can be
	// published after resume; never replay a delivery the source may have
	// executed. Let normal expiry settle it (including grouped results).
	// A paused extra CHECK's deadline moves on resume, so expire it at or
	// before the original pause, not at destination wall time.
	const jobs = `UPDATE checkerjobs j SET
payload = jsonb_set(j.payload, '{generation}', to_jsonb(s.generation)),
publish_token = NULL, publish_deadline_at = NULL, claim_token = NULL, claim_deadline_at = NULL,
deadline_at = CASE WHEN j.sent_at IS NOT NULL THEN LEAST(j.deadline_at, s.paused_at, $1::timestamptz, statement_timestamp())
                   ELSE j.deadline_at END
FROM gamesession s WHERE s.id = 1 AND j.finished_at IS NULL`
	if _, err := tx.Exec(ctx, jobs, m.CreatedAt); err != nil {
		return fmt.Errorf("%s - recover unfinished jobs: %w", location, err)
	}
	return nil
}

func cleanGeneration(ctx context.Context, tx pgx.Tx) (int64, error) {
	const location = "backup.cleanGeneration"
	const query = `SELECT s.generation FROM gamesession s JOIN gameconfig g USING (id)
WHERE NOT g.game_running AND g.real_round = 0 AND s.practice_start IS NULL AND s.final_start IS NULL
AND NOT s.reset_pending AND s.paused_at IS NULL AND COALESCE(s.snapshot_round, 0) = 0
AND NOT EXISTS (SELECT 1 FROM flags) AND NOT EXISTS (SELECT 1 FROM stolenflags)
AND NOT EXISTS (SELECT 1 FROM teamtaskslog) AND NOT EXISTS (SELECT 1 FROM dispatchruns)
AND NOT EXISTS (SELECT 1 FROM gameroundsnapshots) AND NOT EXISTS (SELECT 1 FROM publicannouncements)
AND NOT EXISTS (SELECT 1 FROM checkeractionlogs) AND NOT EXISTS (SELECT 1 FROM backup_team_logos)
AND NOT EXISTS (SELECT 1 FROM schedulehistory)
AND NOT EXISTS (SELECT 1 FROM teamtasks WHERE stolen <> 0 OR lost <> 0 OR checks <> 0 OR checks_passed <> 0)`
	var generation int64
	if err := tx.QueryRow(ctx, query).Scan(&generation); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrTarget
		}
		return 0, fmt.Errorf("%s - check whether destination is empty: %w", location, err)
	}
	return generation, nil
}

func importTable(
	ctx context.Context,
	tx pgx.Tx,
	table table,
	path string,
) (resultErr error) {
	id := pgx.Identifier{table.db}
	rows, err := tx.Query(ctx, "SELECT "+table.columns+" FROM "+id.Sanitize()+" LIMIT 0")
	if err != nil {
		return err
	}
	fields := rows.FieldDescriptions()
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	columns := make([]string, len(fields))
	for i, field := range fields {
		columns[i] = field.Name
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxRowBytes+1)
	values := make([]any, len(fields))
	_, err = tx.CopyFrom(ctx, id, columns, pgx.CopyFromFunc(func() ([]any, error) {
		if !scanner.Scan() {
			return nil, scanner.Err()
		}
		return parseRow(scanner.Bytes(), fields, values)
	}))
	return err
}

func parseRow(data []byte, fields []pgconn.FieldDescription, values []any) ([]any, error) {
	const location = "backup.parseRow"
	var row map[string]json.RawMessage
	if err := json.Unmarshal(data, &row); err != nil || len(row) != len(fields) {
		return nil, fmt.Errorf("%s - row does not match table schema", location)
	}
	for i, field := range fields {
		raw, ok := row[field.Name]
		if !ok {
			return nil, fmt.Errorf("%s - missing column", location)
		}
		value, err := decodeValue(raw, field.DataTypeOID)
		if err != nil {
			return nil, fmt.Errorf("%s - invalid value for column %s", location, field.Name)
		}
		values[i] = value
	}
	return values, nil
}

func decodeValue(raw json.RawMessage, oid uint32) (any, error) {
	const location = "backup.decodeValue"
	if string(raw) == "null" {
		return nil, nil
	}
	switch oid {
	case pgtype.BoolOID:
		return decodeJSON[bool](raw)
	case pgtype.Int4OID:
		return decodeJSON[int32](raw)
	case pgtype.Int8OID:
		return decodeJSON[int64](raw)
	case pgtype.Float8OID:
		return decodeJSON[float64](raw)
	case pgtype.TimestamptzOID:
		return decodeJSON[time.Time](raw)
	case pgtype.UUIDOID:
		return decodeJSON[pgtype.UUID](raw)
	case pgtype.TextOID, pgtype.VarcharOID:
		return decodeJSON[string](raw)
	case pgtype.JSONBOID, pgtype.JSONOID:
		return raw, nil
	default:
		return nil, fmt.Errorf("%s - unsupported backup column type %d", location, oid)
	}
}

func decodeJSON[T any](raw json.RawMessage) (T, error) {
	var value T
	err := json.Unmarshal(raw, &value)
	return value, err
}

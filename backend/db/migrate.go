package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	// Register the pgx driver used by the migration connection.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate is an explicit initializer operation. Application replicas do not
// compete to change the schema while accepting traffic.
func Migrate(ctx context.Context, databaseURL string) (resultErr error) {
	const location = "db.Migrate"
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("%s - open database connection: %w", location, err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("%s - close database connection: %w", location, err))
		}
	}()
	database.SetMaxOpenConns(1)
	if err := validateAppliedSchema(ctx, database); err != nil {
		return fmt.Errorf("%s - validate existing database schema: %w", location, err)
	}
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("%s - locate embedded migrations: %w", location, err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("%s - create migration lock: %w", location, err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, database, files, goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("%s - create migration provider: %w", location, err)
	}
	_, err = provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("%s - apply database migrations: %w", location, err)
	}
	return nil
}

func validateAppliedSchema(ctx context.Context, database *sql.DB) error {
	const location = "db.validateAppliedSchema"
	var versionTableExists bool
	if err := database.QueryRowContext(ctx, `SELECT to_regclass('goose_db_version') IS NOT NULL`).Scan(&versionTableExists); err != nil {
		return fmt.Errorf("%s - check migration ledger: %w", location, err)
	}
	if !versionTableExists {
		return nil
	}

	var versionOneApplied bool
	if err := database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM goose_db_version
			WHERE version_id = 1 AND is_applied
		)
	`).Scan(&versionOneApplied); err != nil {
		return fmt.Errorf("%s - read migration ledger: %w", location, err)
	}
	if !versionOneApplied {
		return nil
	}

	var tableName, columnName string
	var dataType sql.NullString
	err := database.QueryRowContext(ctx, `
		WITH expected(table_name, column_name, data_type) AS (
			VALUES
				('teams', 'id', 'uuid'),
				('tasks', 'id', 'uuid'),
				('flags', 'id', 'uuid'), ('flags', 'team_id', 'uuid'), ('flags', 'task_id', 'uuid'),
				('stolenflags', 'flag_id', 'uuid'), ('stolenflags', 'attacker_id', 'uuid'),
				('teamtasks', 'team_id', 'uuid'), ('teamtasks', 'task_id', 'uuid'),
				('teamtaskslog', 'id', 'uuid'), ('teamtaskslog', 'team_id', 'uuid'), ('teamtaskslog', 'task_id', 'uuid'),
				('dispatchruns', 'id', 'uuid'),
				('checkerjobs', 'id', 'uuid'), ('checkerjobs', 'run_id', 'uuid'),
				('checkerjobs', 'team_id', 'uuid'), ('checkerjobs', 'task_id', 'uuid'),
				('gameconfig', 'extra_checks', 'integer'),
				('checkerjobs', 'not_before', 'timestamp with time zone'),
				('checkerjobs', 'expires_at', 'timestamp with time zone'),
				('checkerjobs', 'aggregate_round', 'boolean'),
				('checkerjobs', 'result', 'jsonb')
		)
		SELECT expected.table_name, expected.column_name, actual.data_type
		FROM expected
		LEFT JOIN information_schema.columns AS actual
		  ON actual.table_schema = current_schema()
		 AND actual.table_name = expected.table_name
		 AND actual.column_name = expected.column_name
		WHERE actual.data_type IS DISTINCT FROM expected.data_type
		ORDER BY expected.data_type <> 'uuid', expected.table_name, expected.column_name
		LIMIT 1
	`).Scan(&tableName, &columnName, &dataType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s - inspect schema columns: %w", location, err)
	}
	actualType := "missing"
	if dataType.Valid {
		actualType = dataType.String
	}
	return fmt.Errorf(
		"%s - applied migration version 1 has incompatible schema: %s.%s has type %s; initialize a fresh database",
		location, tableName, columnName, actualType,
	)
}

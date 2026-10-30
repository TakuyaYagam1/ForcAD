package db_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/TakuyaYagam1/ForcAD/db"
)

func TestInitialMigrationRollbackAndReapply(t *testing.T) {
	fixture := newMigrationFixture(t)
	if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	if _, err := fixture.conn.Exec(fixture.ctx, `
		CREATE TABLE unrelated (value TEXT NOT NULL);
		INSERT INTO unrelated VALUES ('kept');
	`); err != nil {
		t.Fatal(err)
	}

	provider := newMigrationProvider(t, fixture)
	if _, err := provider.DownTo(fixture.ctx, 0); err != nil {
		t.Fatalf("roll back initial migration: %v", err)
	}
	version, err := provider.GetDBVersion(fixture.ctx)
	if err != nil || version != 0 {
		t.Fatalf("version after rollback = %d, error = %v; want 0", version, err)
	}
	var remaining int
	if err := fixture.conn.QueryRow(fixture.ctx, `
		SELECT count(*) FROM pg_class
		WHERE relnamespace = current_schema()::regnamespace
		  AND relkind IN ('r', 'p')
		  AND relname NOT IN ('goose_db_version', 'unrelated')
	`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("rollback left %d application tables", remaining)
	}
	var value string
	if err := fixture.conn.QueryRow(fixture.ctx, `SELECT value FROM unrelated`).Scan(&value); err != nil || value != "kept" {
		t.Fatalf("unrelated data after rollback = %q, error = %v", value, err)
	}
	if _, err := fixture.conn.Exec(fixture.ctx, `DROP TABLE unrelated`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
		t.Fatalf("reapply initial migration: %v", err)
	}
	version, err = provider.GetDBVersion(fixture.ctx)
	if err != nil || version != 1 {
		t.Fatalf("version after reapply = %d, error = %v; want 1", version, err)
	}
	var sessions int
	if err := fixture.conn.QueryRow(fixture.ctx, `SELECT count(*) FROM GameSession WHERE id = 1`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("sessions after reapply = %d, error = %v; want 1", sessions, err)
	}
}

func newMigrationProvider(t *testing.T, fixture *migrationFixture) *goose.Provider {
	t.Helper()
	database, err := sql.Open("pgx", fixture.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	provider, err := goose.NewProvider(goose.DialectPostgres, database,
		os.DirFS("../../db/migrations"), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

package db_test

import (
	"database/sql"
	"os"
	"strings"
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
	if _, err := fixture.conn.Exec(fixture.ctx, `
		INSERT INTO Teams (name, ip, token, kind)
		VALUES ('NPC', '127.0.0.2', 'fedcba9876543210', 'npc')`); err != nil {
		t.Fatalf("create NPC after reapply: %v", err)
	}
	if _, err := fixture.conn.Exec(fixture.ctx, `
		INSERT INTO Teams (name, ip, token, kind)
		VALUES ('second NPC', '127.0.0.3', '1234567890abcdef', 'npc')`); err == nil {
		t.Fatal("second NPC bypassed the single-NPC constraint after reapply")
	}
	if _, err := fixture.conn.Exec(fixture.ctx, `
		INSERT INTO Teams (name, ip, token, kind)
		VALUES ('invalid kind', '127.0.0.4', 'abcdef0123456789', 'observer')`); err == nil {
		t.Fatal("invalid team kind bypassed the check constraint after reapply")
	}
}

func TestMigrateRejectsLegacyVersionOneWithoutNPCSchema(t *testing.T) {
	fixture := newMigrationFixture(t)
	if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	if _, err := fixture.conn.Exec(fixture.ctx, `
		INSERT INTO Teams (name, ip, token)
		VALUES ('existing player', '127.0.0.1', '0123456789abcdef');
		DROP INDEX teams_single_npc;
		ALTER TABLE Teams DROP CONSTRAINT teams_kind_check;
		ALTER TABLE Teams DROP COLUMN kind`); err != nil {
		t.Fatalf("prepare legacy version-one schema: %v", err)
	}

	before := migrationSchemaState(t, fixture)
	err := db.Migrate(fixture.ctx, fixture.dsn)
	if err == nil || !strings.Contains(err.Error(), "teams.kind has type missing") ||
		!strings.Contains(err.Error(), "initialize a fresh database") {
		t.Fatalf("migration error = %v, want missing NPC schema refusal", err)
	}
	if after := migrationSchemaState(t, fixture); after != before {
		t.Fatalf("refusal changed schema, ledger or team data:\nbefore: %s\nafter: %s", before, after)
	}
	var name string
	var version int
	if err := fixture.conn.QueryRow(fixture.ctx, `
		SELECT (SELECT name FROM Teams WHERE token = '0123456789abcdef'),
		       (SELECT max(version_id)::INTEGER FROM goose_db_version WHERE is_applied)`,
	).Scan(&name, &version); err != nil {
		t.Fatal(err)
	}
	if name != "existing player" || version != 1 {
		t.Fatalf("legacy state changed after refusal: team=%q version=%d", name, version)
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

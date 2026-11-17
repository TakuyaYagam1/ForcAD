package db_test

import (
	"testing"

	"github.com/TakuyaYagam1/ForcAD/db"
)

func TestMigrateSquashedBaselinePreservesHistoricalRowsAndLedger(t *testing.T) {
	for _, item := range []struct {
		name     string
		versions []int
	}{
		{"initial_version_one", nil},
		{"former_version_two", []int{2}},
		{"former_version_three", []int{2, 3}},
		{"redaction_version_four", []int{4}},
		{"all_development_versions", []int{2, 3, 4}},
	} {
		t.Run(item.name, func(t *testing.T) {
			fixture := newMigrationFixture(t)
			if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
				t.Fatalf("initialize current schema: %v", err)
			}
			for _, version := range item.versions {
				if _, err := fixture.conn.Exec(fixture.ctx, `
					INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, TRUE)
				`, version); err != nil {
					t.Fatalf("prepare historical ledger version %d: %v", version, err)
				}
			}
			// These synthetic historical rows deliberately bypass current worker
			// persistence. Squashing must not replay version 1 or rewrite data;
			// the public HTTP projection independently protects historical output.
			if _, err := fixture.conn.Exec(fixture.ctx, `
				INSERT INTO CheckerActionLogs
				    (team_id, task_id, round, action, stage, started_at, finished_at, duration_ms, status, public_message)
				VALUES
				    (uuidv7(), uuidv7(), 1, 'put', 'classic', now(), now(), 0, 101, 'synthetic-private-state'),
				    (uuidv7(), uuidv7(), 2, 'put', 'classic', now(), now(), 0, 103, 'public failure'),
				    (uuidv7(), uuidv7(), 3, 'check', 'classic', now(), now(), 0, 101, 'public check');
			`); err != nil {
				t.Fatal(err)
			}
			before := migrationLogState(t, fixture)
			for range 2 {
				if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
					t.Fatalf("repeat migration with historical ledger: %v", err)
				}
				if after := migrationLogState(t, fixture); after != before {
					t.Fatalf("squashed baseline changed logs or migration ledger:\nbefore: %s\nafter: %s", before, after)
				}
			}
		})
	}
}

func migrationLogState(t *testing.T, fixture *migrationFixture) string {
	t.Helper()
	var state string
	if err := fixture.conn.QueryRow(fixture.ctx, `
		SELECT jsonb_build_object(
		    'ledger', (SELECT jsonb_agg(v ORDER BY id) FROM goose_db_version v),
		    'logs', (SELECT jsonb_agg(log ORDER BY round) FROM CheckerActionLogs log)
		)::text
	`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

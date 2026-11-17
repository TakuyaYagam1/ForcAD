package db_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TakuyaYagam1/ForcAD/db"
)

func TestInitialSchemaExtraChecks(t *testing.T) {
	fixture := newMigrationFixture(t)
	if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.conn.Exec(fixture.ctx, `INSERT INTO GameConfig (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	var count int
	var indexes bool
	if err := fixture.conn.QueryRow(fixture.ctx, `
		SELECT extra_checks,
		       to_regclass('checker_jobs_round_group') IS NOT NULL
		         AND to_regclass('checker_jobs_not_before') IS NOT NULL
		FROM GameConfig WHERE id = 1
	`).Scan(&count, &indexes); err != nil || count != 0 || !indexes {
		t.Fatalf("initial extra CHECK state: count=%d indexes=%t err=%v", count, indexes, err)
	}
	for _, item := range []struct {
		name  string
		query string
		code  string
	}{
		{"negative", `UPDATE GameConfig SET extra_checks = -1`, "23514"},
		{"above_limit", `UPDATE GameConfig SET extra_checks = 9`, "23514"},
		{"null_count", `UPDATE GameConfig SET extra_checks = NULL`, "23502"},
		{"blitz", `UPDATE GameConfig SET extra_checks = 2, mode = 'blitz'`, "23514"},
		{"null_mode", `UPDATE GameConfig SET extra_checks = 2, mode = NULL`, "23514"},
	} {
		t.Run(item.name, func(t *testing.T) {
			_, err := fixture.conn.Exec(fixture.ctx, item.query)
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != item.code {
				t.Fatalf("constraint error = %v, want SQLSTATE %s", err, item.code)
			}
		})
	}
	if _, err := fixture.conn.Exec(fixture.ctx, `UPDATE GameConfig SET extra_checks = 8`); err != nil {
		t.Fatalf("valid upper bound: %v", err)
	}
}

func TestMigrateRepeatPreservesExtraCheckState(t *testing.T) {
	for _, item := range []struct {
		name              string
		experimentalEntry bool
	}{
		{"initial_schema", false},
		{"prior_experimental_ledger", true},
	} {
		t.Run(item.name, func(t *testing.T) {
			fixture := newMigrationFixture(t)
			if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.conn.Exec(fixture.ctx, `
				INSERT INTO GameConfig (id, mode, real_round, extra_checks)
				VALUES (1, 'classic', 17, 2);
				INSERT INTO DispatchRuns (schedule_id, round, is_round, round_start)
				VALUES ('classic_rounds', 17, TRUE, 1);
				INSERT INTO CheckerJobs
				    (run_id, round, action, payload, not_before, expires_at, aggregate_round, result)
				SELECT id, 17, 'check_only', '{"round":17}',
				       '2050-01-01T00:00:10Z', '2050-01-01T00:00:50Z', TRUE, '{"status":104}'
				FROM DispatchRuns;
			`); err != nil {
				t.Fatal(err)
			}
			if item.experimentalEntry {
				// The former second migration produced the same columns and indexes.
				if _, err := fixture.conn.Exec(fixture.ctx, `
					INSERT INTO goose_db_version (version_id, is_applied) VALUES (2, TRUE)
				`); err != nil {
					t.Fatal(err)
				}
			}
			before := extraCheckState(t, fixture)
			if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
				t.Fatalf("repeat migration: %v", err)
			}
			if after := extraCheckState(t, fixture); after != before {
				t.Fatalf("repeat migration changed game, jobs or ledger:\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}

func TestMigrateRejectsMissingExtraCheckColumnWithoutChangingData(t *testing.T) {
	for _, item := range []struct {
		column string
		drop   string
	}{
		{"gameconfig.extra_checks", `ALTER TABLE GameConfig DROP COLUMN extra_checks`},
		{"checkerjobs.not_before", `ALTER TABLE CheckerJobs DROP COLUMN not_before`},
		{"checkerjobs.expires_at", `ALTER TABLE CheckerJobs DROP COLUMN expires_at`},
		{"checkerjobs.aggregate_round", `ALTER TABLE CheckerJobs DROP COLUMN aggregate_round`},
		{"checkerjobs.result", `ALTER TABLE CheckerJobs DROP COLUMN result`},
	} {
		t.Run(item.column, func(t *testing.T) {
			fixture := newMigrationFixture(t)
			if err := db.Migrate(fixture.ctx, fixture.dsn); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.conn.Exec(fixture.ctx, `INSERT INTO GameConfig (id, real_round) VALUES (1, 17)`); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.conn.Exec(fixture.ctx, item.drop); err != nil {
				t.Fatal(err)
			}
			before := extraCheckState(t, fixture)
			err := db.Migrate(fixture.ctx, fixture.dsn)
			if err == nil || !strings.Contains(err.Error(), item.column+" has type missing") ||
				!strings.Contains(err.Error(), "initialize a fresh database") {
				t.Fatalf("migration error = %v, want missing-column refusal", err)
			}
			if after := extraCheckState(t, fixture); after != before {
				t.Fatalf("refusal changed existing data:\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}

func extraCheckState(t *testing.T, fixture *migrationFixture) string {
	t.Helper()
	var state string
	if err := fixture.conn.QueryRow(fixture.ctx, `
		SELECT jsonb_build_object(
		    'game', (SELECT jsonb_agg(g ORDER BY id) FROM GameConfig AS g),
		    'jobs', (SELECT jsonb_agg(j ORDER BY id) FROM CheckerJobs AS j),
		    'ledger', (SELECT jsonb_agg(v ORDER BY id) FROM goose_db_version AS v)
		)::TEXT
	`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

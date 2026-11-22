package postgres_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/backup"
)

func TestBackupRestoresFinishedBoard(t *testing.T) {
	source, _ := testutil.Database(t)
	target, _ := testutil.Database(t)
	ctx := t.Context()
	for _, pool := range []*pgxpool.Pool{source, target} {
		if _, err := postgres.NewGameStore(pool).Initialize(ctx, gameDocument(t)); err != nil {
			t.Fatal(err)
		}
	}
	const logo = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><path d="M 0 0 H 1"/></svg>`
	logos := t.TempDir()
	if err := os.WriteFile(filepath.Join(logos, "first.svg"), []byte(logo), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := backup.New(source, logos)
	if _, err := writer.Export(ctx, &bytes.Buffer{}, "finals"); !errors.Is(err, backup.ErrSource) {
		t.Fatalf("export while waiting: %v", err)
	}
	if _, err := source.Exec(ctx, `
UPDATE teams SET name = 'Restored team', logo_path = '/team-logos/first.svg' WHERE token = '0000000000000001';
UPDATE gameconfig SET real_round = 2;
UPDATE gamesession SET snapshot_round = 2, snapshot = '{"round":2,"team_tasks":[]}', final_start = now(), paused_at = now();
UPDATE teamtasks SET score = 2542.25, checks = 2, checks_passed = 1;
INSERT INTO flags (flag, team_id, task_id, round, public_flag_data, private_flag_data)
SELECT 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=', teams.id, tasks.id, 1, '', 'private' FROM teams CROSS JOIN tasks
WHERE teams.token = '0000000000000001';
INSERT INTO stolenflags (flag_id, attacker_id, attacker_delta)
SELECT flags.id, teams.id, 42.25 FROM flags CROSS JOIN teams WHERE teams.token = '0000000000000002';
INSERT INTO publicannouncements (title, markdown, author) VALUES ('Finished','Results posted','organizer');
`); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := writer.Export(ctx, &buf, "finals"); err != nil {
		t.Fatal(err)
	}
	reader := backup.New(target, t.TempDir())
	if info, err := reader.Inspect(ctx, bytes.NewReader(buf.Bytes())); err != nil || info.Name != "finals" || info.Round != 2 {
		t.Fatalf("inspect = %+v, %v", info, err)
	}
	if _, err := reader.Restore(ctx, bytes.NewReader(buf.Bytes()[:buf.Len()-4])); !errors.Is(err, backup.ErrInvalid) {
		t.Fatalf("restore corrupt archive: %v", err)
	}
	if _, err := target.Exec(ctx, `ALTER TABLE publicannouncements ADD CONSTRAINT test_reject_author CHECK (author <> 'organizer')`); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Restore(ctx, bytes.NewReader(buf.Bytes())); !errors.Is(err, backup.ErrInvalid) {
		t.Fatalf("restore despite a destination constraint failure: %v", err)
	}
	var original string
	if err := target.QueryRow(ctx, `SELECT name FROM teams WHERE token = '0000000000000001'`).Scan(&original); err != nil || original != "first" {
		t.Fatalf("failed restore changed target name to %q: %v", original, err)
	}
	var importedFlags int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM flags`).Scan(&importedFlags); err != nil || importedFlags != 0 {
		t.Fatalf("failed restore left %d imported flags: %v", importedFlags, err)
	}
	if _, err := target.Exec(ctx, `ALTER TABLE publicannouncements DROP CONSTRAINT test_reject_author`); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Restore(ctx, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := target.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM teams t JOIN teamtasks c ON c.team_id = t.id
JOIN flags f ON f.team_id=t.id AND f.task_id=c.task_id JOIN stolenflags s ON s.flag_id=f.id
WHERE t.name='Restored team' AND t.token='0000000000000001'
AND t.logo_path='/api/v1/client/team-logos/first.svg' AND f.private_flag_data='private'
AND c.score=2542.25 AND c.checks=2 AND c.checks_passed=1 AND s.attacker_delta=42.25
) AND EXISTS (SELECT 1 FROM publicannouncements WHERE markdown='Results posted')`).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("restored rows inconsistent: %v %v", preserved, err)
	}
	contentType, content, err := reader.Logo(ctx, "first.svg")
	if err != nil || contentType != "image/svg+xml" || string(content) != logo {
		t.Fatalf("restored logo is missing or changed: %q %v", contentType, err)
	}
	if _, err := reader.Restore(ctx, bytes.NewReader(buf.Bytes())); !errors.Is(err, backup.ErrTarget) {
		t.Fatalf("restored over a finished game: %v", err)
	}
	if _, err := reader.Export(ctx, &bytes.Buffer{}, "imported"); err != nil {
		t.Fatalf("re-export without the original logo directory: %v", err)
	}
}

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/backup"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func restoreBackup(t *testing.T, source *pgxpool.Pool) (*pgxpool.Pool, backup.Manifest) {
	t.Helper()
	ctx := t.Context()
	target, _ := testutil.Database(t)
	if _, err := postgres.NewGameStore(target).Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	exported, err := backup.New(source, t.TempDir()).Export(ctx, &buf, "event")
	if err != nil {
		t.Fatal(err)
	}
	reader := backup.New(target, t.TempDir())
	if m, err := reader.Inspect(ctx, bytes.NewReader(buf.Bytes())); err != nil || m.Round != exported.Round || !m.CreatedAt.Equal(exported.CreatedAt) {
		t.Fatalf("inspect: %+v, %v", m, err)
	}
	if _, err := reader.Restore(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	return target, exported
}

func TestBackupRoundZero(t *testing.T) {
	for _, tc := range []struct {
		name, phase string
		practice    bool
		actions     []string
	}{
		{
			name:  "waiting",
			phase: "waiting",
		},
		{
			name:     "rehearsal",
			phase:    "paused",
			practice: true,
			actions:  []string{"start_practice"},
		},
		{
			name:    "paused final",
			phase:   "paused",
			actions: []string{"start_final", "pause"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			source, _ := testutil.Database(t)
			if tc.phase == "waiting" {
				if _, err := backup.New(source, t.TempDir()).Export(ctx, &bytes.Buffer{}, "empty"); !errors.Is(err, backup.ErrSource) {
					t.Fatalf("uninitialized export: %v", err)
				}
			}
			doc := gameDocument(t)
			doc.Teams[0].Name = "Archived team"
			game := postgres.NewGameStore(source)
			if _, err := game.Initialize(ctx, doc); err != nil {
				t.Fatal(err)
			}
			for _, action := range tc.actions {
				if err := game.ChangeState(ctx, action, nil, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			target, m := restoreBackup(t, source)
			board, err := postgres.NewGameStore(target).Board(ctx)
			if err != nil {
				t.Fatal(err)
			}
			life := board.Lifecycle
			status := life.Runtime(time.Now())
			if m.Round != 0 || life.Config.RealRound != 0 || status.Phase != tc.phase ||
				status.Practice != tc.practice || life.Session.SnapshotRound == nil ||
				*life.Session.SnapshotRound != 0 || len(life.Session.Snapshot) == 0 ||
				len(board.Teams) != 2 || len(board.Cells) != 2 {
				t.Fatalf("restored round zero: %+v", life)
			}
			var name string
			if err := target.QueryRow(ctx, `SELECT name FROM teams WHERE token='0000000000000001'`).Scan(&name); err != nil || name != "Archived team" {
				t.Fatalf("team not restored: %q, %v", name, err)
			}
			original, err := game.Lifecycle(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !life.Config.StartTime.Equal(original.Config.StartTime) || life.Session.Generation <= original.Session.Generation {
				t.Fatal("restore changed schedule or failed to fence the destination")
			}
			switch tc.name {
			case "rehearsal":
				if original.Session.PausedAt != nil || life.Session.PausedAt == nil || !life.Session.PausedAt.Equal(m.CreatedAt) {
					t.Fatal("running rehearsal was not paused at snapshot time on the destination only")
				}
			case "paused final":
				if original.Session.PausedAt == nil || life.Session.PausedAt == nil || !life.Session.PausedAt.Equal(*original.Session.PausedAt) {
					t.Fatal("restoring a paused final changed its clock")
				}
			}
		})
	}
}

func TestBackupRestoreDuringOutboxPoll(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	store := backup.New(pool, t.TempDir())
	var archive bytes.Buffer
	if _, err := store.Export(ctx, &archive, "waiting"); err != nil {
		t.Fatal(err)
	}

	// Pause ClaimPending between its lifecycle reads and its outbox update.
	poll, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer poll.Rollback(t.Context())
	q := sqlc.New(poll)
	if _, err := q.LockGameForAttack(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := q.LockSessionForAttack(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restored := make(chan error, 1)
	go func() {
		defer close(restored)
		_, err := store.Restore(ctx, &archive)
		restored <- err
	}()
	t.Cleanup(func() {
		<-restored
	})
	waitForBlockedTransaction(t, pool, int32(poll.Conn().PgConn().PID()))

	now := time.Now()
	owner := "ticker"
	jobs, err := q.ClaimOutboxJobs(ctx, sqlc.ClaimOutboxJobsParams{
		OwnerToken: &owner,
		ClaimedAt: pgtype.Timestamptz{
			Time:  now,
			Valid: true,
		},
		ClaimDeadline: pgtype.Timestamptz{
			Time:  now.Add(time.Minute),
			Valid: true,
		},
		JobLimit: 1,
	})
	if err != nil || len(jobs) != 0 {
		t.Fatalf("poll on waiting board during restore: %d jobs, %v", len(jobs), err)
	}
	if err := poll.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-restored; err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil || life.Session.Generation != session.Generation+1 {
		t.Fatalf("restore did not fence the previous generation: %+v, %v", life.Session, err)
	}
}

func TestBackupRecoversRunningJobs(t *testing.T) {
	ctx := t.Context()
	source, _ := testutil.Database(t)
	game := postgres.NewGameStore(source)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Second)
	if err := game.ChangeState(ctx, "start_final", nil, start); err != nil {
		t.Fatal(err)
	}
	repo := postgres.New(source)
	run, err := repo.Prepare(ctx, domain.ScheduleRequest{
		ScheduleID:    "classic_rounds",
		At:            start,
		StartAt:       start,
		Interval:      time.Minute,
		AdvancesRound: true,
		Workflow:      domain.JobActionClassic,
	})
	if err != nil || run == nil {
		t.Fatalf("prepare: %v, %v", run, err)
	}
	deadline := start.Add(time.Hour)
	jobs, err := repo.ClaimPending(ctx, "publisher", start, deadline, 2)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("outbox: %d, %v", len(jobs), err)
	}
	if ok, err := repo.MarkPublished(ctx, jobs[0].ID, "publisher", start, deadline); err != nil || !ok {
		t.Fatalf("publish: %t, %v", ok, err)
	}
	if _, err := repo.Claim(ctx, jobs[0].ID, "source", start, deadline); err != nil {
		t.Fatal(err)
	}
	target, m := restoreBackup(t, source)
	dest := postgres.New(target)
	life, err := postgres.NewGameStore(target).Lifecycle(ctx)
	if err != nil || m.Round != 1 || life.Session.PausedAt == nil || !life.Session.PausedAt.Equal(m.CreatedAt) ||
		life.Session.RoundStartedAt == nil || !life.Session.RoundStartedAt.Equal(start) {
		t.Fatalf("restored clock: %+v, %v", life, err)
	}
	if jobs, err := dest.ClaimPending(ctx, "destination", m.CreatedAt, deadline, 2); err != nil || len(jobs) != 0 {
		t.Fatalf("paused outbox: %d, %v", len(jobs), err)
	}
	if ok, err := dest.Expire(ctx, jobs[0].ID, m.CreatedAt); err != nil || !ok {
		t.Fatalf("lost broker delivery did not expire: %t, %v", ok, err)
	}
	resumed := m.CreatedAt.Add(24 * time.Hour)
	session, err := dest.SetPaused(ctx, false, resumed)
	if err != nil || session.PausedSeconds != 86400 || session.SchedulePausedSeconds != 86400 {
		t.Fatalf("resumed clock: %+v, %v", session, err)
	}
	pending, err := dest.ClaimPending(ctx, "destination", resumed, resumed.Add(time.Minute), 2)
	if err != nil || len(pending) != 1 || pending[0].ID != jobs[1].ID {
		t.Fatalf("only never-published job should return to outbox: %+v, %v", pending, err)
	}
	if state, err := repo.State(ctx, jobs[0].ID); err != nil || state.FinishedAt != nil || state.PutStartedAt == nil {
		t.Fatalf("export changed source job: %+v, %v", state, err)
	}
}

func TestBackupRecoversFinishedChecks(t *testing.T) {
	ctx := t.Context()
	f := newExtraCheckFixture(t)
	main := f.claim(t, f.start, 1)[0]
	const down = 104
	f.complete(t, main, f.start.Add(time.Second), down)
	extras := f.claim(t, f.start.Add(49*time.Second), 2)
	if err := postgres.NewGameStore(f.pool).ChangeState(ctx, "finish", nil, f.start.Add(50*time.Second)); err != nil {
		t.Fatal(err)
	}
	target, _ := restoreBackup(t, f.pool)
	dest := postgres.New(target)
	at := f.start.Add(time.Minute)
	ids, err := dest.ExpiredJobIDs(ctx, at, 10)
	if err != nil || len(ids) != len(extras) {
		t.Fatalf("lost published checks not eligible for recovery: %v, %v", ids, err)
	}
	for _, id := range ids {
		if ok, err := dest.Expire(ctx, id, at); err != nil || !ok {
			t.Fatalf("expire: %t, %v", ok, err)
		}
	}
	restored := extraCheckFixture{
		pool: target,
	}
	restored.assertScore(t, down, 1, 0)
	if ok, err := dest.Maintain(ctx, at); err != nil || !ok {
		t.Fatalf("final scoreboard did not settle: %t, %v", ok, err)
	}
	life, err := postgres.NewGameStore(target).Lifecycle(ctx)
	if err != nil || !life.Finished() || life.Pending || life.Session.SnapshotRound == nil || *life.Session.SnapshotRound != 1 {
		t.Fatalf("restored final: %+v, %v", life, err)
	}
}

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

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestNewV7ID(t *testing.T) {
	id := uuid.NewV7()
	if version := id[6] >> 4; version != 7 {
		t.Fatalf("UUID version = %d, want 7", version)
	}
	if variant := id[8] & 0xc0; variant != 0x80 {
		t.Fatalf("UUID variant bits = %02x, want 80", variant)
	}
}

func TestDispatchRunUsesStoredSnapshotMetadata(t *testing.T) {
	snapshotRound := int32(7)
	snapshotRoundStart := float64(1234)
	scheduledAt := time.Unix(10, 0)
	runID := uuid.NewV7()
	run, err := dispatchRunFromRow(sqlc.Dispatchrun{
		ID: runID, ScheduleID: "classic_rounds", Round: 10, IsRound: true,
		RoundStart: 99, Snapshot: []byte(`[]`), SnapshotRound: &snapshotRound,
		SnapshotRoundStart: &snapshotRoundStart,
		ScheduledAt:        pgtype.Timestamptz{Time: scheduledAt, Valid: true},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !run.ScheduledAt.Equal(scheduledAt) {
		t.Fatalf("dispatch scheduled_at = %s, want %s", run.ScheduledAt, scheduledAt)
	}
	if run.Snapshot == nil || run.Snapshot.Round != snapshotRound || run.Snapshot.RoundStart != int64(snapshotRoundStart) {
		t.Fatalf("dispatch snapshot metadata = %+v, want round %d start %d", run.Snapshot, snapshotRound, int64(snapshotRoundStart))
	}
}

func TestDispatchRunRejectsMissingRoundSnapshotData(t *testing.T) {
	snapshotRound := int32(7)
	snapshotRoundStart := float64(1234)
	runID := uuid.NewV7()
	base := sqlc.Dispatchrun{
		ID: runID, ScheduleID: "classic_rounds", Round: 10, IsRound: true,
		Snapshot: []byte(`[]`), SnapshotRound: &snapshotRound,
		SnapshotRoundStart: &snapshotRoundStart,
		ScheduledAt:        pgtype.Timestamptz{Time: time.Unix(10, 0), Valid: true},
	}
	tests := []struct {
		name    string
		row     sqlc.Dispatchrun
		message string
	}{
		{name: "missing snapshot round", row: base, message: "snapshot metadata"},
		{name: "missing snapshot start", row: base, message: "snapshot metadata"},
		{name: "missing snapshot", row: base, message: "snapshot data"},
		{name: "missing scheduled time", row: base, message: "scheduled time"},
	}
	tests[0].row.SnapshotRound = nil
	tests[1].row.SnapshotRoundStart = nil
	tests[2].row.Snapshot = nil
	tests[3].row.ScheduledAt = pgtype.Timestamptz{}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := dispatchRunFromRow(test.row, 1); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("dispatchRunFromRow() error = %v, want message containing %q", err, test.message)
			}
		})
	}
}

func TestPrepareRequiresExplicitWorkflow(t *testing.T) {
	at := time.Unix(10, 0)
	var repository Repository
	_, err := repository.Prepare(context.Background(), domain.ScheduleRequest{
		ScheduleID: "classic_rounds", At: at, StartAt: at, Interval: time.Minute,
	})
	if err == nil {
		t.Fatal("Prepare() accepted a request without a workflow")
	}
}

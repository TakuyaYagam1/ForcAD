package postgres

import (
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCheckerJobUsesRequiredDatabaseIdentityAndPreservesPayload(t *testing.T) {
	action := string(domain.JobActionClassic)
	id, runID := uuid.NewV7(), uuid.NewV7()
	teamID, taskID := uuid.NewV7(), uuid.NewV7()
	payload := []byte(`{"team":{"id":11},"task":{"id":13},"workflow":"puts_only","extension":{"value":true}}`)

	job, err := makeCheckerJobForTest(id, runID, 4, &action, &teamID, &taskID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != id || job.RunID != runID || job.Round != 4 || job.Action != domain.JobActionClassic ||
		job.TeamID != teamID || job.TaskID != taskID {
		t.Fatalf("checker job identity = %+v", job)
	}
	if string(job.Payload) != string(payload) {
		t.Fatalf("payload = %s, want unchanged bytes %s", job.Payload, payload)
	}
}

func TestCheckerJobRejectsMissingOrInvalidDatabaseFields(t *testing.T) {
	classic := string(domain.JobActionClassic)
	invalidWorkflow := "unknown"
	id, runID := uuid.NewV7(), uuid.NewV7()
	teamID, taskID := uuid.NewV7(), uuid.NewV7()
	zero := uuid.Nil()
	payload := []byte(`{"team":{"id":7},"task":{"id":3},"workflow":"classic"}`)

	tests := []struct {
		name      string
		id, runID uuid.UUID
		round     int32
		action    *string
		teamID    *uuid.UUID
		taskID    *uuid.UUID
		wantErr   error
	}{
		{name: "missing job id", id: uuid.Nil(), runID: runID, round: 4, action: &classic, teamID: &teamID, taskID: &taskID, wantErr: errInvalidCheckerJobIdentity},
		{name: "missing run id", id: id, runID: uuid.Nil(), round: 4, action: &classic, teamID: &teamID, taskID: &taskID, wantErr: errInvalidCheckerJobIdentity},
		{name: "negative round", id: id, runID: runID, round: -1, action: &classic, teamID: &teamID, taskID: &taskID, wantErr: errInvalidCheckerJobIdentity},
		{name: "missing action despite payload workflow", id: id, runID: runID, round: 4, teamID: &teamID, taskID: &taskID, wantErr: domain.ErrInvalidJobWorkflow},
		{name: "empty action", id: id, runID: runID, round: 4, action: new(""), teamID: &teamID, taskID: &taskID, wantErr: domain.ErrInvalidJobWorkflow},
		{name: "unknown action", id: id, runID: runID, round: 4, action: &invalidWorkflow, teamID: &teamID, taskID: &taskID, wantErr: domain.ErrInvalidJobWorkflow},
		{name: "missing team id despite payload id", id: id, runID: runID, round: 4, action: &classic, taskID: &taskID, wantErr: domain.ErrTeamTaskMissing},
		{name: "missing task id despite payload id", id: id, runID: runID, round: 4, action: &classic, teamID: &teamID, wantErr: domain.ErrTeamTaskMissing},
		{name: "zero team id", id: id, runID: runID, round: 4, action: &classic, teamID: &zero, taskID: &taskID, wantErr: domain.ErrTeamTaskMissing},
		{name: "zero task id", id: id, runID: runID, round: 4, action: &classic, teamID: &teamID, taskID: &zero, wantErr: domain.ErrTeamTaskMissing},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := makeCheckerJobForTest(test.id, test.runID, test.round, test.action, test.teamID, test.taskID, payload)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("makeCheckerJob() error = %v, want %v", err, test.wantErr)
			}
			if !strings.HasPrefix(err.Error(), "postgres.makeCheckerJob - ") {
				t.Fatalf("error is missing its source: %v", err)
			}
		})
	}
}

func makeCheckerJobForTest(
	id, runID uuid.UUID,
	round int32,
	action *string,
	teamID, taskID *uuid.UUID,
	payload []byte,
) (domain.CheckerJob, error) {
	return makeCheckerJob(
		id, runID, round, action, teamID, taskID, payload,
		pgtype.Timestamptz{}, pgtype.Timestamptz{}, pgtype.Timestamptz{},
		nil, pgtype.Timestamptz{}, nil, pgtype.Timestamptz{}, pgtype.Timestamptz{},
	)
}

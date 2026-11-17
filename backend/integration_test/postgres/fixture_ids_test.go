package postgres_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
)

func gameEntityIDs(ctx context.Context, t *testing.T, store *postgres.GameStore) ([]uuid.UUID, []uuid.UUID) {
	t.Helper()
	teams, err := store.Teams(ctx, false)
	if err != nil {
		t.Fatalf("load test teams: %v", err)
	}
	tasks, err := store.Tasks(ctx, false)
	if err != nil {
		t.Fatalf("load test tasks: %v", err)
	}
	teamIDs := make([]uuid.UUID, len(teams))
	for i, team := range teams {
		teamIDs[i] = team.ID
	}
	taskIDs := make([]uuid.UUID, len(tasks))
	for i, task := range tasks {
		taskIDs[i] = task.ID
	}
	return teamIDs, taskIDs
}

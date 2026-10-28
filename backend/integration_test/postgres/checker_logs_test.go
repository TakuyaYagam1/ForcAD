package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase"
)

func TestCheckerLogsPageUsesTeamTaskCursorAndPublicFields(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := context.Background()
	store := postgres.NewGameStore(pool)
	if _, err := store.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, store)
	started := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	ids := make([]uuid.UUID, 3)
	for index, message := range []string{"first public", "second public", "third public"} {
		err := pool.QueryRow(ctx, `
			INSERT INTO CheckerActionLogs
				(team_id, task_id, round, action, stage, started_at, finished_at,
				 duration_ms, timed_out, status, public_message)
			VALUES ($1, $2, 4, 'check', 'classic', $3::TIMESTAMPTZ,
			        $3::TIMESTAMPTZ + interval '5 milliseconds', 5, FALSE, 101, $4)
			RETURNING id`, teams[0], tasks[0], started.Add(time.Duration(index)*time.Second), message,
		).Scan(&ids[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO CheckerActionLogs
			(team_id, task_id, round, action, stage, started_at, finished_at,
			 duration_ms, timed_out, status, public_message)
		VALUES ($1, $2, 4, 'get', 'classic', $3, $3, 0, FALSE, 101, 'other team')`,
		teams[1], tasks[0], started,
	); err != nil {
		t.Fatal(err)
	}

	service := usecase.NewGame(store, nil, nil, nil)
	defer service.Close()
	page, err := service.CheckerLogs(ctx, teams[0], tasks[0], nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Limit != 2 || !page.HasMore || len(page.Logs) != 2 || page.NextBeforeID == nil {
		t.Fatalf("first page = %+v", page)
	}
	if page.Logs[0].ID != ids[2] || page.Logs[1].ID != ids[1] || *page.NextBeforeID != ids[1] {
		t.Fatalf("first page order or cursor: %+v", page)
	}
	last, err := service.CheckerLogs(ctx, teams[0], tasks[0], page.NextBeforeID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if last.HasMore || len(last.Logs) != 1 || last.Logs[0].ID != ids[0] || last.NextBeforeID != nil {
		t.Fatalf("last page = %+v", last)
	}

	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	publicJSON := string(encoded)
	for _, privateField := range []string{"private_message", "command", "flag", "private data"} {
		if strings.Contains(publicJSON, privateField) {
			t.Fatalf("private field %q in public response: %s", privateField, publicJSON)
		}
	}
	var hasPrivateColumns bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'checkeractionlogs'
			  AND column_name IN ('private_message', 'command', 'flag', 'private_data')
		)`).Scan(&hasPrivateColumns); err != nil {
		t.Fatal(err)
	}
	if hasPrivateColumns {
		t.Fatal("public checker log table contains a private field")
	}
}

func TestCheckerLogTypeContainsOnlyPublicValues(t *testing.T) {
	encoded, err := json.Marshal(domain.CheckerLog{
		ID: uuid.MustParse("01912345-6789-7000-8000-000000000001"), Round: 3,
		Action: "put", Stage: "classic", Status: 101,
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
		PublicMessage: "OK",
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"private_message", "command", "flag", "private_data"} {
		if _, exists := fields[field]; exists {
			t.Fatalf("checker log exposes %q", field)
		}
	}
}

package usecase

import (
	"context"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type checkerLogRepository struct {
	outbound.GameRepository

	rows       []domain.CheckerLog
	beforeID   *uuid.UUID
	queryLimit int32
}

func (r *checkerLogRepository) CheckerLogs(_ context.Context, _, _ uuid.UUID, beforeID *uuid.UUID, limit int32) ([]domain.CheckerLog, error) {
	r.beforeID = beforeID
	r.queryLimit = limit
	return r.rows, nil
}

func TestCheckerLogsBuildsCursorPage(t *testing.T) {
	teamID := uuid.MustParse("01912345-6789-7000-8000-000000000001")
	taskID := uuid.MustParse("01912345-6789-7000-8000-000000000002")
	firstID := uuid.MustParse("01912345-6789-7000-8000-000000000003")
	secondID := uuid.MustParse("01912345-6789-7000-8000-000000000004")
	thirdID := uuid.MustParse("01912345-6789-7000-8000-000000000005")
	beforeID := uuid.MustParse("01912345-6789-7000-8000-000000000006")
	repo := &checkerLogRepository{
		rows: []domain.CheckerLog{{ID: firstID}, {ID: secondID}, {ID: thirdID}},
	}
	service := NewGame(repo, nil, nil, nil)
	defer service.Close()

	page, err := service.CheckerLogs(context.Background(), teamID, taskID, &beforeID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if repo.queryLimit != 3 || repo.beforeID == nil || *repo.beforeID != beforeID {
		t.Fatalf("repository query = limit %d before %v", repo.queryLimit, repo.beforeID)
	}
	if page.TeamID != teamID || page.TaskID != taskID || page.Limit != 2 || !page.HasMore ||
		page.NextBeforeID == nil || *page.NextBeforeID != secondID {
		t.Fatalf("page metadata = %+v", page)
	}
	if len(page.Logs) != 2 || page.Logs[0].ID != firstID || page.Logs[1].ID != secondID {
		t.Fatalf("page logs = %+v", page.Logs)
	}
}

func TestCheckerLogsUsesDefaultAndMaximumLimits(t *testing.T) {
	teamID := uuid.MustParse("01912345-6789-7000-8000-000000000001")
	taskID := uuid.MustParse("01912345-6789-7000-8000-000000000002")
	tests := []struct {
		name      string
		requested int32
		wantLimit int32
	}{
		{name: "default", requested: 0, wantLimit: 100},
		{name: "maximum", requested: 501, wantLimit: 500},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &checkerLogRepository{}
			service := NewGame(repo, nil, nil, nil)
			defer service.Close()

			page, err := service.CheckerLogs(context.Background(), teamID, taskID, nil, test.requested)
			if err != nil {
				t.Fatal(err)
			}
			if page.Limit != test.wantLimit || repo.queryLimit != test.wantLimit+1 {
				t.Fatalf("page limit %d and query limit %d, want %d and %d", page.Limit, repo.queryLimit, test.wantLimit, test.wantLimit+1)
			}
			if page.Logs == nil || page.HasMore || page.NextBeforeID != nil {
				t.Fatalf("empty page metadata = %+v", page)
			}
		})
	}
}

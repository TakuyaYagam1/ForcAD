package outbound

import (
	"context"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type SchedulerRepository interface {
	Prepare(ctx context.Context, request domain.ScheduleRequest) (*domain.DispatchRun, error)
	Finish(ctx context.Context, runID uuid.UUID, at time.Time) (bool, error)
	StartScheduled(ctx context.Context, at time.Time) (bool, error)
	Maintain(ctx context.Context, at time.Time) (bool, error)
	ExpiredJobIDs(ctx context.Context, at time.Time, limit int32) ([]uuid.UUID, error)
	CompletedSnapshots(ctx context.Context, afterRound, limit int32) ([]domain.RoundSnapshot, error)
	LastRun(ctx context.Context, scheduleID string) (*time.Time, error)
}

type JobPublisher interface {
	Publish(ctx context.Context, jobIDs []uuid.UUID) (confirmed []uuid.UUID, err error)
}

package outbound

import (
	"context"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

// SchedulerRepository composes dispatch runs with the game and job state needed
// to advance them. The entity contracts remain usable independently.
type SchedulerRepository interface {
	GameMaintenanceRepository
	JobRecoveryRepository
	RoundSnapshotReader

	Prepare(ctx context.Context, request domain.ScheduleRequest) (*domain.DispatchRun, error)
	Finish(ctx context.Context, runID uuid.UUID, at time.Time) (bool, error)
	LastRun(ctx context.Context, scheduleID string) (*time.Time, error)
}

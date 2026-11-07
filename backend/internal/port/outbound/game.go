package outbound

import (
	"context"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type GameRepository interface {
	TeamCatalogRepository
	TaskRepository
	ScoreRepository
	CheckerLogRepository

	Lifecycle(ctx context.Context) (domain.Lifecycle, error)
	ChangeState(
		ctx context.Context,
		action string,
		generation *int64,
		at time.Time,
	) error
}

type GameMaintenanceRepository interface {
	StartScheduled(ctx context.Context, at time.Time) (bool, error)
	Maintain(ctx context.Context, at time.Time) (bool, error)
}

package outbound

import (
	"context"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type TaskRepository interface {
	Tasks(ctx context.Context, active bool) ([]domain.Task, error)
	CreateTask(ctx context.Context, task domain.Task) (domain.Task, error)
	UpdateTask(ctx context.Context, task domain.Task) (domain.Task, error)
	DeactivateTask(ctx context.Context, taskID uuid.UUID) error
}

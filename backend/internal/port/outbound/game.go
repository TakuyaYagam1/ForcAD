package outbound

import (
	"context"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type GameRepository interface {
	Board(ctx context.Context) (domain.BoardData, error)
	Lifecycle(ctx context.Context) (domain.Lifecycle, error)
	Teams(ctx context.Context, active bool) ([]domain.Team, error)
	Tasks(ctx context.Context, active bool) ([]domain.Task, error)
	Cells(ctx context.Context) ([]domain.Cell, error)
	History(ctx context.Context, teamID uuid.UUID, taskID *uuid.UUID) ([]domain.Cell, error)
	PublicHistory(ctx context.Context, teamID uuid.UUID) ([]domain.PublicCell, error)
	ChangeState(ctx context.Context, action string, generation *int64, at time.Time) error
	CreateTeam(ctx context.Context, team domain.Team) (domain.Team, error)
	UpdateTeam(ctx context.Context, team domain.Team) (domain.Team, error)
	DeactivateTeam(ctx context.Context, teamID uuid.UUID) error
	CreateTask(ctx context.Context, task domain.Task) (domain.Task, error)
	UpdateTask(ctx context.Context, task domain.Task) (domain.Task, error)
	DeactivateTask(ctx context.Context, taskID uuid.UUID) error
}

package v1

import (
	"context"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase"
)

type GameService interface {
	Lifecycle(ctx context.Context) (domain.Lifecycle, error)
	Runtime(ctx context.Context) (domain.RuntimeStatus, error)
	Teams(ctx context.Context, public bool) ([]domain.Team, error)
	Tasks(ctx context.Context, public bool) ([]domain.Task, error)
	History(ctx context.Context, teamID uuid.UUID, taskID *uuid.UUID) ([]domain.Cell, error)
	PublicHistory(ctx context.Context, teamID uuid.UUID) ([]domain.PublicCell, error)
	Scoreboard(ctx context.Context) (domain.Scoreboard, error)
	Standings(ctx context.Context) ([]usecase.Standing, error)
	AttackData(ctx context.Context) (map[string]map[string][]string, error)
	ChangeState(ctx context.Context, action string, generation *int64) error
	SaveTeam(ctx context.Context, team domain.Team, create bool) (domain.Team, error)
	SaveTask(ctx context.Context, task domain.Task, create bool) (domain.Task, error)
	DeleteTeam(ctx context.Context, teamID uuid.UUID) error
	DeleteTask(ctx context.Context, taskID uuid.UUID) error
}

type AuthService interface {
	Login(ctx context.Context, username, password string) (string, error)
	Status(ctx context.Context, token string) (string, error)
	Logout(ctx context.Context, token string) error
}

type FlagService interface {
	Submit(ctx context.Context, token string, values []string) ([]usecase.FlagResponse, error)
}

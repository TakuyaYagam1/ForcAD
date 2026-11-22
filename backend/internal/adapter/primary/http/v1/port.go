package v1

import (
	"context"
	"io"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/backup"
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
	CheckerLogs(
		ctx context.Context,
		teamID, taskID uuid.UUID,
		beforeID *uuid.UUID,
		limit int32,
	) (domain.CheckerLogPage, error)
	Scoreboard(ctx context.Context) (domain.Scoreboard, error)
	Standings(ctx context.Context) ([]usecase.Standing, error)
	AttackData(ctx context.Context, service *string) (map[string]map[string][]string, error)
	ChangeState(ctx context.Context, action string, generation *int64) error
	UpdateGameSettings(ctx context.Context, input domain.GameSettingsUpdate) (domain.Lifecycle, error)
	SaveTeam(ctx context.Context, team domain.Team, create bool) (domain.Team, error)
	SaveTask(ctx context.Context, task domain.Task, create bool) (domain.Task, error)
	DeleteTeam(ctx context.Context, teamID uuid.UUID) error
	DeleteTask(ctx context.Context, taskID uuid.UUID) error
	DeactivateTeams(ctx context.Context, ids []uuid.UUID) (int32, error)
	DeactivateTasks(ctx context.Context, ids []uuid.UUID) (int32, error)
	ActivateTeams(ctx context.Context, ids []uuid.UUID) (int32, error)
	ActivateTasks(ctx context.Context, ids []uuid.UUID) (int32, error)
}

type AuthService interface {
	Login(ctx context.Context, username, password string) (string, error)
	StatusDetails(ctx context.Context, token string) (username, csrfToken string, err error)
	Logout(ctx context.Context, token string) error
}

type FlagService interface {
	Submit(ctx context.Context, token string, values []string) ([]usecase.FlagResponse, error)
}

type BackupService interface {
	Export(ctx context.Context, out io.Writer, name string) (backup.Manifest, error)
	Inspect(ctx context.Context, input io.Reader) (backup.Manifest, error)
	Restore(ctx context.Context, input io.Reader) (backup.Manifest, error)
	Logo(ctx context.Context, name string) (string, []byte, error)
}

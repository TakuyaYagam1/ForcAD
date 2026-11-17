package outbound

import (
	"context"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type ScoreRepository interface {
	Board(ctx context.Context) (domain.BoardData, error)
	Cells(ctx context.Context) ([]domain.Cell, error)
	History(ctx context.Context, teamID uuid.UUID, taskID *uuid.UUID) ([]domain.Cell, error)
	PublicHistory(ctx context.Context, teamID uuid.UUID) ([]domain.PublicCell, error)
}

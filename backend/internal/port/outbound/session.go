package outbound

import (
	"context"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type SessionRepository interface {
	Get(ctx context.Context) (domain.Session, error)
	SetPaused(ctx context.Context, paused bool, at time.Time) (domain.Session, error)
	SaveSnapshot(ctx context.Context, snapshot domain.RoundSnapshot) error
}

type RoundSnapshotReader interface {
	CompletedSnapshots(ctx context.Context, afterRound, limit int32) ([]domain.RoundSnapshot, error)
}

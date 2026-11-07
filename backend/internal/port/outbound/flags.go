package outbound

import (
	"context"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type FlagRepository interface {
	Create(ctx context.Context, flag domain.Flag, owner *domain.CheckerJobOwner) (domain.Flag, error)
	FindLiveByValue(ctx context.Context, value string, currentRound int32) (domain.Flag, error)
	FindLiveByID(ctx context.Context, id uuid.UUID, currentRound int32) (domain.Flag, error)
	FindLiveByValues(ctx context.Context, values []string, currentRound int32) (map[string]domain.Flag, error)
	FindRandomRound(
		ctx context.Context,
		teamID, taskID uuid.UUID,
		round int32,
	) (domain.Flag, error)
	AttackData(ctx context.Context, currentRound int32, taskIDs []uuid.UUID) ([]domain.AttackDatum, error)
}

type AttackRepository interface {
	SubmitBatch(ctx context.Context, submission domain.FlagSubmission) ([]domain.SubmissionResult, error)
}

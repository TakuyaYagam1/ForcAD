package outbound

import (
	"context"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type JobRepository interface {
	Claim(
		ctx context.Context,
		jobID uuid.UUID,
		ownerToken string,
		at, claimDeadline time.Time,
	) (domain.CheckerJob, error)
	State(ctx context.Context, jobID uuid.UUID) (domain.CheckerJobState, error)
	Renew(
		ctx context.Context,
		jobID uuid.UUID,
		ownerToken string,
		at, claimDeadline time.Time,
	) (bool, error)
	ExtendDeadline(
		ctx context.Context,
		jobID uuid.UUID,
		ownerToken string,
		at, deadline time.Time,
	) (bool, error)
	SetDeadline(
		ctx context.Context,
		jobID uuid.UUID,
		ownerToken string,
		at, deadline time.Time,
	) (bool, error)
	Complete(
		ctx context.Context,
		jobID uuid.UUID,
		ownerToken string,
		at time.Time,
		result domain.CheckerResult,
	) (bool, error)
	Expire(ctx context.Context, jobID uuid.UUID, at time.Time) (bool, error)
}

type OutboxRepository interface {
	ClaimPending(
		ctx context.Context,
		ownerToken string,
		at, claimDeadline time.Time,
		limit int32,
	) ([]domain.CheckerJob, error)
	MarkPublished(
		ctx context.Context,
		jobID uuid.UUID,
		ownerToken string,
		at, deadline time.Time,
	) (bool, error)
}

type JobRecoveryRepository interface {
	ExpiredJobIDs(ctx context.Context, at time.Time, limit int32) ([]uuid.UUID, error)
}

type JobPublisher interface {
	Publish(ctx context.Context, jobIDs []uuid.UUID) (confirmed []uuid.UUID, err error)
}

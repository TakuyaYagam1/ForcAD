package outbound

import (
	"context"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type TeamRepository interface {
	ActiveIDByToken(ctx context.Context, token string) (uuid.UUID, error)
	IsActiveTeam(ctx context.Context, teamID uuid.UUID) (bool, error)
}

type FlagRepository interface {
	Create(ctx context.Context, flag domain.Flag, owner *domain.CheckerJobOwner) (domain.Flag, error)
	FindLiveByValue(ctx context.Context, value string, currentRound int32) (domain.Flag, error)
	FindLiveByID(ctx context.Context, id uuid.UUID, currentRound int32) (domain.Flag, error)
	FindLiveByValues(ctx context.Context, values []string, currentRound int32) (map[string]domain.Flag, error)
	FindRandomRound(ctx context.Context, teamID, taskID uuid.UUID, round int32) (domain.Flag, error)
	AttackData(ctx context.Context, currentRound int32, taskIDs []uuid.UUID) ([]domain.AttackDatum, error)
}

type AttackRepository interface {
	SubmitBatch(ctx context.Context, submission domain.FlagSubmission) ([]domain.SubmissionResult, error)
}

type SessionRepository interface {
	Get(ctx context.Context) (domain.Session, error)
	SetPaused(ctx context.Context, paused bool, at time.Time) (domain.Session, error)
	SaveSnapshot(ctx context.Context, snapshot domain.RoundSnapshot) error
}

type JobRepository interface {
	Claim(ctx context.Context, jobID uuid.UUID, ownerToken string, at, claimDeadline time.Time) (domain.CheckerJob, error)
	State(ctx context.Context, jobID uuid.UUID) (domain.CheckerJobState, error)
	Renew(ctx context.Context, jobID uuid.UUID, ownerToken string, at, claimDeadline time.Time) (bool, error)
	ExtendDeadline(ctx context.Context, jobID uuid.UUID, ownerToken string, at, deadline time.Time) (bool, error)
	SetDeadline(ctx context.Context, jobID uuid.UUID, ownerToken string, at, deadline time.Time) (bool, error)
	Complete(ctx context.Context, jobID uuid.UUID, ownerToken string, at time.Time, result domain.CheckerResult) (bool, error)
	Expire(ctx context.Context, jobID uuid.UUID, at time.Time) (bool, error)
}

type OutboxRepository interface {
	ClaimPending(ctx context.Context, ownerToken string, at, claimDeadline time.Time, limit int32) ([]domain.CheckerJob, error)
	MarkPublished(ctx context.Context, jobID uuid.UUID, ownerToken string, at, deadline time.Time) (bool, error)
}

type EventRepository interface {
	ListEvents(ctx context.Context, after string, limit int) ([]domain.Event, error)
	CreateAnnouncement(ctx context.Context, title, markdown, author string) (domain.Event, error)
}

package domain

import (
	"errors"
	"fmt"
	"math"
	"time"
	"uuid"
)

var (
	ErrTeamTokenInvalid     = errors.New("team token is invalid")
	ErrFlagNotFound         = errors.New("flag is invalid")
	ErrFlagTooOld           = errors.New("flag is too old")
	ErrOwnFlag              = errors.New("flag belongs to the submitting team")
	ErrFlagAlreadyStolen    = errors.New("flag already stolen")
	ErrGameUnavailable      = errors.New("game is not available")
	ErrCheckerJobClosed     = errors.New("checker job is closed")
	ErrServiceDown          = errors.New("service is down")
	ErrInvalidGameConfig    = errors.New("game scoring configuration is invalid")
	ErrTeamTaskMissing      = errors.New("team task score is missing")
	ErrJobClaimed           = errors.New("checker job is already claimed or PUT has started")
	ErrJobNotReady          = errors.New("checker job is not ready for execution")
	ErrInvalidJobWorkflow   = errors.New("checker job workflow is invalid")
	ErrInvalidCheckerResult = errors.New("checker result is invalid")
)

const (
	TaskStatusUp          int32 = 101
	TaskStatusCheckFailed int32 = 110
)

type Flag struct {
	ID              uuid.UUID
	Value           string
	TeamID          uuid.UUID
	TaskID          uuid.UUID
	Round           int32
	PublicData      string
	PrivateData     string
	VulnerabilityID *int32
}

func (f Flag) IsLive(currentRound, lifetime int32) bool {
	return lifetime > 0 && int64(currentRound)-int64(f.Round) <= int64(lifetime)
}

type AttackDatum struct {
	TaskID     uuid.UUID
	TeamIP     string
	PublicData string
}

type AttackResult struct {
	AttackerID    uuid.UUID
	VictimID      uuid.UUID
	TaskID        uuid.UUID
	AttackerDelta float64
	VictimDelta   float64
	Generation    int64
}

type FlagSubmission struct {
	TeamID     uuid.UUID
	Values     []string
	ReceivedAt time.Time
	Generation int64
}

// SubmissionResult is returned in input order, only after the batch commits.
// Err contains a flag verdict; infrastructure errors fail the whole batch.
type SubmissionResult struct {
	Attack AttackResult
	Err    error
}

type RatingChange struct {
	Attacker float64
	Victim   float64
}

func CalculateRatingChange(
	attackerScore, victimScore, hardness float64, inflation bool,
) (RatingChange, error) {
	const location = "domain.CalculateRatingChange"
	if attackerScore < 0 || victimScore < 0 || hardness <= 1 ||
		isInvalidFloat(attackerScore) || isInvalidFloat(victimScore) || isInvalidFloat(hardness) {
		return RatingChange{}, fmt.Errorf("%s - validate scores and hardness: %w", location, ErrInvalidGameConfig)
	}

	scale := 50 * math.Sqrt(hardness)
	norm := math.Log(math.Log(hardness)) / 12
	attackerDelta := scale / (1 + math.Exp((math.Sqrt(attackerScore)-math.Sqrt(victimScore))*norm))
	victimDelta := -math.Min(victimScore, attackerDelta)
	if !inflation {
		attackerDelta = math.Min(attackerDelta, -victimDelta)
	}

	return RatingChange{Attacker: attackerDelta, Victim: victimDelta}, nil
}

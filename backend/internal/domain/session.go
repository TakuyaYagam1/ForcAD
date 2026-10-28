package domain

import (
	"encoding/json"
	"time"
	"uuid"
)

type Session struct {
	PracticeStart         *time.Time
	FinalStart            *time.Time
	ResetPending          bool
	Generation            int64
	PausedAt              *time.Time
	PausedSeconds         float64
	SchedulePausedSeconds float64
	RoundStartedAt        *time.Time
	SnapshotRound         *int32
	Snapshot              json.RawMessage
}

type JobAction string

const (
	JobActionClassic   JobAction = "classic"
	JobActionPutsOnly  JobAction = "puts_only"
	JobActionCheckGets JobAction = "check_gets"
	JobActionCheckOnly JobAction = "check_only"
)

type CheckerJobOwner struct {
	JobID      uuid.UUID
	ClaimToken string
}

type CheckerJob struct {
	ID                uuid.UUID
	RunID             uuid.UUID
	Round             int32
	Action            JobAction
	TeamID            uuid.UUID
	TaskID            uuid.UUID
	Payload           json.RawMessage
	SentAt            *time.Time
	FinishedAt        *time.Time
	DeadlineAt        *time.Time
	PublishToken      *string
	PublishDeadlineAt *time.Time
	ClaimToken        *string
	ClaimDeadlineAt   *time.Time
	PutStartedAt      *time.Time
	NotBefore         *time.Time
	ExpiresAt         *time.Time
}

type CheckerJobState struct {
	Action          JobAction
	SentAt          *time.Time
	FinishedAt      *time.Time
	DeadlineAt      *time.Time
	ClaimToken      *string
	ClaimDeadlineAt *time.Time
	PutStartedAt    *time.Time
}

type CheckerResult struct {
	Round          int32        `json:"round"`
	Status         int32        `json:"status"`
	PublicMessage  string       `json:"public_message"`
	PrivateMessage string       `json:"private_message"`
	Command        string       `json:"command"`
	Logs           []CheckerLog `json:"-"`
}

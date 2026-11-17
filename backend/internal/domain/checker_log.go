package domain

import (
	"time"
	"uuid"
)

// CheckerLog contains the fields exposed by the public checker history.
type CheckerLog struct {
	ID            uuid.UUID `json:"id"`
	Round         int32     `json:"round"`
	Action        string    `json:"action"`
	Stage         string    `json:"stage"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	DurationMS    int64     `json:"duration_ms"`
	TimedOut      bool      `json:"timed_out"`
	Status        int32     `json:"status"`
	PublicMessage string    `json:"public_message"`
}

// PublicView excludes successful PUT output, which can be private retrieval
// state. This also protects historical rows whose checker protocol is unknown.
// Deliberately public pfr hints belong in Flag.PublicData, not action logs.
func (log CheckerLog) PublicView() CheckerLog {
	if log.Action == "put" && log.Status == TaskStatusUp {
		log.PublicMessage = "PUT completed"
	}
	return log
}

type CheckerLogPage struct {
	TeamID       uuid.UUID    `json:"team_id"`
	TaskID       uuid.UUID    `json:"task_id"`
	Limit        int32        `json:"limit"`
	HasMore      bool         `json:"has_more"`
	NextBeforeID *uuid.UUID   `json:"next_before_id,omitempty"`
	Logs         []CheckerLog `json:"logs"`
}

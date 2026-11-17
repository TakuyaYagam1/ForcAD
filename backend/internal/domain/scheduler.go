package domain

import (
	"encoding/json"
	"fmt"
	"time"
	"uuid"
)

type ScheduleRequest struct {
	ScheduleID    string
	At            time.Time
	StartAt       time.Time
	Interval      time.Duration
	AdvancesRound bool
	Workflow      JobAction
	TaskID        *uuid.UUID
}

type RoundSnapshot struct {
	Round      int32        `json:"round"`
	RoundStart int64        `json:"round_start"`
	TeamTasks  []PublicCell `json:"team_tasks"`
}

type DispatchRun struct {
	ID          uuid.UUID
	ScheduleID  string
	Round       int32
	IsRound     bool
	RoundStart  float64
	ScheduledAt time.Time
	Snapshot    *RoundSnapshot
	Completed   bool
	Generation  int64
	Workflow    JobAction
}

func (r DispatchRun) SnapshotJSON() ([]byte, error) {
	const location = "domain.DispatchRun.SnapshotJSON"
	if r.Snapshot == nil {
		return nil, nil
	}
	data, err := json.Marshal(r.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("%s - encode round snapshot: %w", location, err)
	}
	return data, nil
}

package domain

import (
	"time"
	"uuid"
)

type EventKind string

const (
	EventFlagStolen   EventKind = "flag_stolen"
	EventAdminMessage EventKind = "admin_message"
)

type Event struct {
	ID            string     `json:"id"`
	Kind          EventKind  `json:"kind"`
	CreatedAt     time.Time  `json:"created_at"`
	AttackerID    *uuid.UUID `json:"attacker_id,omitempty"`
	AttackerName  *string    `json:"attacker_name,omitempty"`
	AttackerDelta *float64   `json:"attacker_delta,omitempty"`
	VictimID      *uuid.UUID `json:"victim_id,omitempty"`
	VictimName    *string    `json:"victim_name,omitempty"`
	TaskID        *uuid.UUID `json:"task_id,omitempty"`
	TaskName      *string    `json:"task_name,omitempty"`
	Round         *int32     `json:"round,omitempty"`
	Title         *string    `json:"title,omitempty"`
	Markdown      *string    `json:"markdown,omitempty"`
	Author        *string    `json:"author,omitempty"`
}

func FlagEventID(flagID, attackerID uuid.UUID) string {
	return string(EventFlagStolen) + ":" + flagID.String() + ":" + attackerID.String()
}

func AnnouncementEventID(id uuid.UUID) string {
	return string(EventAdminMessage) + ":" + id.String()
}

package domain

import "uuid"

type CheckerActionCount struct {
	Maximum  int64
	Reported int64
	Failed   int64
	NoResult int64
}

type TaskCheckerStats struct {
	TaskID uuid.UUID
	Name   string
	Check  CheckerActionCount
	Put    CheckerActionCount
	Get    CheckerActionCount
}

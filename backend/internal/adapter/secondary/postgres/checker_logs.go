package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func (g *GameStore) CheckerLogs(
	ctx context.Context,
	teamID, taskID uuid.UUID,
	beforeID *uuid.UUID,
	limit int32,
) ([]domain.CheckerLog, error) {
	const location = "postgres.GameStore.CheckerLogs"
	rows, err := g.queries.ReadPublicCheckerLogs(ctx, sqlc.ReadPublicCheckerLogsParams{
		TeamID: teamID, TaskID: taskID, BeforeID: beforeID, LogLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - read public checker logs: %w", location, err)
	}
	result := make([]domain.CheckerLog, 0, len(rows))
	for _, row := range rows {
		log := domain.CheckerLog{
			ID: row.ID, Round: row.Round, Action: row.Action, Stage: row.Stage,
			StartedAt: row.StartedAt.Time, FinishedAt: row.FinishedAt.Time,
			DurationMS: row.DurationMs, TimedOut: row.TimedOut, Status: row.Status,
			PublicMessage: row.PublicMessage,
		}
		result = append(result, log.PublicView())
	}
	return result, nil
}

func insertCheckerLogs(
	ctx context.Context,
	queries *sqlc.Queries,
	teamID, taskID uuid.UUID,
	round int32,
	logs []domain.CheckerLog,
) error {
	const location = "postgres.insertCheckerLogs"
	for index, log := range logs {
		if !validCheckerLog(log, round) {
			return fmt.Errorf("%s - validate checker log %d: %w", location, index, domain.ErrInvalidCheckerResult)
		}
		log = log.PublicView()
		if err := queries.InsertPublicCheckerLog(ctx, sqlc.InsertPublicCheckerLogParams{
			TeamID: teamID, TaskID: taskID, Round: log.Round,
			Action: log.Action, Stage: log.Stage, StartedAt: gameStamp(log.StartedAt),
			FinishedAt: gameStamp(log.FinishedAt), DurationMs: log.DurationMS,
			TimedOut: log.TimedOut, Status: log.Status, PublicMessage: log.PublicMessage,
		}); err != nil {
			return fmt.Errorf("%s - insert checker log %d: %w", location, index, err)
		}
	}
	return nil
}

func validCheckerLog(log domain.CheckerLog, round int32) bool {
	if round < 0 || log.Round != round || log.StartedAt.IsZero() || log.FinishedAt.IsZero() ||
		log.FinishedAt.Before(log.StartedAt) || log.DurationMS < 0 {
		return false
	}
	switch log.Action {
	case "check", "put", "get":
	default:
		return false
	}
	switch domain.JobAction(log.Stage) {
	case domain.JobActionClassic, domain.JobActionPutsOnly, domain.JobActionCheckGets, domain.JobActionCheckOnly:
	default:
		return false
	}
	switch log.Status {
	case 101, 102, 103, 104, 110:
		return true
	default:
		return false
	}
}

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func validateExtraCheckTimeout(ctx context.Context, queries *sqlc.Queries, task domain.Task) error {
	const location = "postgres.validateExtraCheckTimeout"
	game, err := queries.LockSchedulerGame(ctx)
	if err != nil {
		return fmt.Errorf("%s - read game settings: %w", location, err)
	}
	if game.ExtraChecks > 0 && int64(task.CheckerTimeout)+3 >= int64(game.RoundTime) {
		return fmt.Errorf("%s - checker cannot fit in round: %w", location, &domain.ValidationError{
			Message: "Extra checks require round_time > checker_timeout + 3",
		})
	}
	return nil
}

// Each extra CHECK gets a random instant in a separate part of the available
// round window. Two seconds remain after its full action timeout for completion.
func addExtraChecks(jobs []schedulerJobRecord, at time.Time, roundTime, count int32, random io.Reader) ([]schedulerJobRecord, error) {
	const location = "postgres.addExtraChecks"
	if count < 0 || count > 8 || at.IsZero() || roundTime <= 0 {
		return nil, fmt.Errorf("%s - invalid extra check settings", location)
	}
	if count == 0 {
		return jobs, nil
	}
	result := make([]schedulerJobRecord, 0, len(jobs)*(int(count)+1))
	for _, job := range jobs {
		var payload domain.JobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, fmt.Errorf("%s - decode main job: %w", location, err)
		}
		if job.Action != string(domain.JobActionClassic) || payload.Task.CheckerTimeout <= 0 ||
			int64(payload.Task.CheckerTimeout)+3 >= int64(roundTime) {
			return nil, fmt.Errorf("%s - require classic mode and round_time > checker_timeout + 3", location)
		}
		job.AggregateRound = true
		result = append(result, job)
		window := time.Duration(int64(roundTime)-int64(payload.Task.CheckerTimeout)-3) * time.Second
		slot := window / time.Duration(count)
		payload.Workflow = string(domain.JobActionCheckOnly)
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("%s - encode extra check: %w", location, err)
		}
		for index := range count {
			offset, err := rand.Int(random, big.NewInt(int64(slot)))
			if err != nil {
				return nil, fmt.Errorf("%s - choose extra check time: %w", location, err)
			}
			start := at.Add(time.Second + time.Duration(index)*slot + time.Duration(offset.Int64()))
			end := at.Add(time.Duration(roundTime) * time.Second)
			result = append(result, schedulerJobRecord{
				RunID: job.RunID, Round: job.Round, TeamID: job.TeamID, TaskID: job.TaskID,
				Action: string(domain.JobActionCheckOnly), Payload: body,
				NotBefore: &start, ExpiresAt: &end, AggregateRound: true,
			})
		}
	}
	return result, nil
}

// The caller locks the shared TeamTasks row before completing a grouped job.
// This serializes completion and expiry without taking other jobs' row locks.
func groupedResult(ctx context.Context, queries *sqlc.Queries, row sqlc.CompleteCheckerJobClaimRow) (domain.CheckerResult, bool, bool, error) {
	const location = "postgres.groupedResult"
	results, err := queries.GroupedCheckerResults(ctx, sqlc.GroupedCheckerResultsParams{
		RunID: row.RunID, TeamID: row.TeamID, TaskID: row.TaskID,
	})
	if err != nil {
		return domain.CheckerResult{}, false, false, fmt.Errorf("%s - read round results: %w", location, err)
	}
	result, ready, counted, err := aggregateResults(results)
	result.Round = row.Round
	return result, ready, counted, err
}

func aggregateResults(rows []sqlc.GroupedCheckerResultsRow) (domain.CheckerResult, bool, bool, error) {
	const location = "postgres.aggregateResults"
	if len(rows) == 0 {
		return domain.CheckerResult{}, false, false, fmt.Errorf("%s - empty round group", location)
	}
	selected := domain.CheckerResult{Status: domain.TaskStatusUp, PublicMessage: "OK"}
	for _, row := range rows {
		if !row.FinishedAt.Valid {
			return domain.CheckerResult{}, false, false, nil
		}
		current := domain.CheckerResult{Status: domain.TaskStatusCheckFailed}
		if len(row.Result) > 0 {
			if err := json.Unmarshal(row.Result, &current); err != nil {
				return domain.CheckerResult{}, false, false, fmt.Errorf("%s - decode result: %w", location, err)
			}
		}
		// A confirmed service failure takes priority over missing observations.
		// Otherwise incomplete infrastructure work is excluded from SLA.
		if selected.Status == domain.TaskStatusUp ||
			(selected.Status == domain.TaskStatusCheckFailed && current.Status != domain.TaskStatusUp) {
			selected = current
		}
	}
	return selected, true, selected.Status != domain.TaskStatusCheckFailed, nil
}

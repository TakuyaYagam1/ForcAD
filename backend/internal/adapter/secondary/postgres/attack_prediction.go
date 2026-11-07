package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type attackPrediction struct {
	batch   attackBatch
	results []domain.SubmissionResult
	changes sqlc.ApplySubmissionChangesParams
}

func (b *attackBatch) predictChanges() (*attackPrediction, bool) {
	inserted := make(map[uuid.UUID]bool, len(b.flags))
	for _, flag := range b.flags {
		if flag.err == nil {
			inserted[flag.row.ID] = true
		}
	}
	if len(inserted) == 0 {
		return nil, false
	}
	prediction := &attackPrediction{
		batch: *b,
	}
	prediction.batch.scores = make(map[teamTaskKey]*attackScore, len(b.scores))
	for key, score := range b.scores {
		copied := *score
		prediction.batch.scores[key] = &copied
	}
	results, err := prediction.batch.scoreResults(inserted)
	if err != nil {
		// A speculative flag may lose an insert conflict. Only calculations for
		// flags actually inserted may reject the batch, so retain the old path.
		return nil, false
	}
	changes, err := prediction.batch.changeParams(results)
	if err != nil {
		return nil, false
	}
	prediction.results, prediction.changes = results, changes
	return prediction, true
}

func (b *attackBatch) applyPredicted(ctx context.Context, queries *sqlc.Queries, prediction *attackPrediction) ([]domain.SubmissionResult, error) {
	const location = "postgres.attackBatch.applyPredicted"
	changes := prediction.changes
	row, err := queries.InsertSubmissionChanges(ctx, sqlc.InsertSubmissionChangesParams{
		AttackerID: b.submission.TeamID,
		ReceivedAt: timestamp(b.submission.ReceivedAt),
		FlagIds:    changes.FlagIds,
		Deltas:     changes.Deltas,
		TeamIds:    changes.TeamIds,
		TaskIds:    changes.TaskIds,
		Stolen:     changes.Stolen,
		Lost:       changes.Lost,
		Scores:     changes.Scores,
	})
	if err != nil {
		return nil, fmt.Errorf("%s - insert flags and predicted scores: %w", location, err)
	}
	inserted, err := checkedInsertedFlags(changes.FlagIds, row.FlagIds)
	if err != nil {
		return nil, fmt.Errorf("%s - validate accepted flags: %w", location, err)
	}
	if len(inserted) == len(changes.FlagIds) {
		if row.ScoreRows != int64(len(changes.TeamIds)) {
			return nil, fmt.Errorf("%s - update all predicted scores: %w", location, domain.ErrTeamTaskMissing)
		}
		b.scores = prediction.batch.scores
		return prediction.results, nil
	}
	if row.ScoreRows != 0 {
		return nil, fmt.Errorf("%s - partial insert changed predicted scores", location)
	}
	// Conflicts can appear after ReadSubmissionFlags while acquiring score
	// locks. Original scores remain untouched. Recompute the actual inserted
	// set in input order, then correct provisional event deltas in a separate
	// statement in this same transaction before any result can be committed.
	return b.applyInserted(ctx, queries, inserted)
}

func checkedInsertedFlags(expected, actual []uuid.UUID) (map[uuid.UUID]bool, error) {
	const location = "postgres.checkedInsertedFlags"
	wanted := make(map[uuid.UUID]bool, len(expected))
	for _, id := range expected {
		if wanted[id] {
			return nil, fmt.Errorf("%s - duplicate expected flag ID", location)
		}
		wanted[id] = true
	}
	inserted := make(map[uuid.UUID]bool, len(actual))
	for _, id := range actual {
		if !wanted[id] || inserted[id] {
			return nil, fmt.Errorf("%s - unexpected or duplicate inserted flag ID", location)
		}
		inserted[id] = true
	}
	return inserted, nil
}

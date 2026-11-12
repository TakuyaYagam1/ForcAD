package postgres

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type attackFlag struct {
	row sqlc.ReadSubmissionFlagsRow
	err error
}

func (f *attackFlag) npc() bool {
	return f.row.Kind == domain.TeamKindNPC
}

type teamTaskKey struct {
	team, task uuid.UUID
}

type attackScore struct {
	status int32
	score  float64
	stolen int32
	lost   int32
}

type attackBatch struct {
	submission domain.FlagSubmission
	state      submissionState
	flags      map[string]*attackFlag
	scores     map[teamTaskKey]*attackScore
}

func newAttackBatch(submission domain.FlagSubmission, state submissionState, rows []sqlc.ReadSubmissionFlagsRow) *attackBatch {
	batch := &attackBatch{
		submission: submission,
		state:      state,
		flags:      make(map[string]*attackFlag, len(rows)),
		scores:     make(map[teamTaskKey]*attackScore),
	}
	for _, row := range rows {
		flag := &attackFlag{
			row: row,
		}
		switch {
		case row.Stolen:
			flag.err = domain.ErrFlagAlreadyStolen
		case row.TeamID == submission.TeamID:
			flag.err = domain.ErrOwnFlag
		case row.Round > state.round:
			flag.err = domain.ErrFlagNotFound
		case !(domain.Flag{
			Round: row.Round,
		}).IsLive(state.round, state.game.FlagLifetime):
			flag.err = domain.ErrFlagTooOld
		}
		batch.flags[row.Flag] = flag
	}
	return batch
}

func (b *attackBatch) lockTasks(ctx context.Context, queries *sqlc.Queries) error {
	const location = "postgres.attackBatch.lockTasks"
	wanted := make(map[teamTaskKey]bool)
	for _, flag := range b.flags {
		if flag.err != nil {
			continue
		}
		if flag.npc() {
			if b.state.game.VolgaAttacksMode {
				wanted[teamTaskKey{b.submission.TeamID, flag.row.TaskID}] = true
			}
			continue
		}
		wanted[teamTaskKey{b.submission.TeamID, flag.row.TaskID}] = true
		wanted[teamTaskKey{flag.row.TeamID, flag.row.TaskID}] = true
	}
	if len(wanted) == 0 {
		return nil
	}
	params := sqlc.LockSubmissionTasksParams{}
	for key := range wanted {
		params.TeamIds = append(params.TeamIds, key.team)
		params.TaskIds = append(params.TaskIds, key.task)
	}
	// SQL locks every pair in (team, task) order, including opposing batches.
	rows, err := queries.LockSubmissionTasks(ctx, params)
	if err != nil {
		return fmt.Errorf("%s - lock team tasks: %w", location, err)
	}
	for _, row := range rows {
		b.scores[teamTaskKey{row.TeamID, row.TaskID}] = &attackScore{
			status: row.Status,
			score:  row.Score,
		}
	}
	for _, flag := range b.flags {
		if flag.err != nil {
			continue
		}
		attacker := b.scores[teamTaskKey{b.submission.TeamID, flag.row.TaskID}]
		if flag.npc() {
			if b.state.game.VolgaAttacksMode && (attacker == nil || attacker.status != domain.TaskStatusUp) {
				flag.err = domain.ErrServiceDown
			}
			continue
		}
		victim := b.scores[teamTaskKey{flag.row.TeamID, flag.row.TaskID}]
		if b.state.game.VolgaAttacksMode && (attacker == nil || attacker.status != domain.TaskStatusUp) {
			flag.err = domain.ErrServiceDown
		} else if attacker == nil || victim == nil {
			return fmt.Errorf("%s - find team task: %w", location, domain.ErrTeamTaskMissing)
		}
	}
	return nil
}

func (b *attackBatch) apply(ctx context.Context, queries *sqlc.Queries) ([]domain.SubmissionResult, error) {
	const location = "postgres.attackBatch.apply"
	if prediction, ok := b.predictChanges(); ok {
		return b.applyPredicted(ctx, queries, prediction)
	}
	ids := make([]uuid.UUID, 0, len(b.flags))
	for _, flag := range b.flags {
		if flag.err == nil {
			ids = append(ids, flag.row.ID)
		}
	}
	inserted := make(map[uuid.UUID]bool, len(ids))
	if len(ids) > 0 {
		rows, err := queries.InsertStolenFlags(ctx, sqlc.InsertStolenFlagsParams{
			AttackerID: b.submission.TeamID,
			FlagIds:    ids,
			ReceivedAt: timestamp(b.submission.ReceivedAt),
		})
		if err != nil {
			return nil, fmt.Errorf("%s - record stolen flags: %w", location, err)
		}
		for _, id := range rows {
			inserted[id] = true
		}
	}
	return b.applyInserted(ctx, queries, inserted)
}

func (b *attackBatch) scoreResults(inserted map[uuid.UUID]bool) ([]domain.SubmissionResult, error) {
	const location = "postgres.attackBatch.scoreResults"
	results := make([]domain.SubmissionResult, len(b.submission.Values))
	for i, value := range b.submission.Values {
		result, err := b.scoreFlag(value, inserted)
		if err != nil {
			return nil, fmt.Errorf("%s - calculate flag score: %w", location, err)
		}
		results[i] = result
	}
	return results, nil
}

func (b *attackBatch) applyInserted(ctx context.Context, queries *sqlc.Queries, inserted map[uuid.UUID]bool) ([]domain.SubmissionResult, error) {
	const location = "postgres.attackBatch.applyInserted"
	results, err := b.scoreResults(inserted)
	if err != nil {
		return nil, fmt.Errorf("%s - score inserted flags: %w", location, err)
	}
	if err := b.saveChanges(ctx, queries, results); err != nil {
		return nil, fmt.Errorf("%s - save scores and event deltas: %w", location, err)
	}
	return results, nil
}

func (b *attackBatch) scoreFlag(value string, inserted map[uuid.UUID]bool) (domain.SubmissionResult, error) {
	const location = "postgres.attackBatch.scoreFlag"
	flag := b.flags[value]
	if flag == nil {
		return domain.SubmissionResult{
			Err: domain.ErrFlagNotFound,
		}, nil
	}
	if flag.err != nil {
		//nolint:nilerr // Expected flag verdicts are data; transaction errors use the error return.
		return domain.SubmissionResult{
			Err: flag.err,
		}, nil
	}
	if !inserted[flag.row.ID] {
		return domain.SubmissionResult{
			Err: domain.ErrFlagAlreadyStolen,
		}, nil
	}
	delete(inserted, flag.row.ID)
	if flag.npc() {
		return domain.SubmissionResult{
			NPC: true,
		}, nil
	}
	attacker := b.scores[teamTaskKey{b.submission.TeamID, flag.row.TaskID}]
	victim := b.scores[teamTaskKey{flag.row.TeamID, flag.row.TaskID}]
	// Rating depends on the preceding flag's score. Preserve input order while
	// writing the final row values once, without adding rounded deltas in SQL.
	change, err := domain.CalculateRatingChange(attacker.score, victim.score, b.state.game.GameHardness, b.state.game.Inflation)
	if err != nil {
		return domain.SubmissionResult{}, fmt.Errorf("%s - calculate rating: %w", location, err)
	}
	attacker.score += change.Attacker
	attacker.stolen++
	victim.score += change.Victim
	victim.lost++
	return domain.SubmissionResult{
		Attack: domain.AttackResult{
			AttackerID:    b.submission.TeamID,
			VictimID:      flag.row.TeamID,
			TaskID:        flag.row.TaskID,
			AttackerDelta: change.Attacker,
			VictimDelta:   change.Victim,
			Generation:    b.state.generation,
			EventID:       domain.FlagEventID(flag.row.ID, b.submission.TeamID),
			CreatedAt:     b.submission.ReceivedAt,
		},
	}, nil
}

func (b *attackBatch) changeParams(results []domain.SubmissionResult) (sqlc.ApplySubmissionChangesParams, error) {
	const location = "postgres.attackBatch.changeParams"
	params := sqlc.ApplySubmissionChangesParams{
		AttackerID: b.submission.TeamID,
	}
	for key, score := range b.scores {
		if score.stolen == 0 && score.lost == 0 {
			continue
		}
		params.TeamIds = append(params.TeamIds, key.team)
		params.TaskIds = append(params.TaskIds, key.task)
		params.Stolen = append(params.Stolen, score.stolen)
		params.Lost = append(params.Lost, score.lost)
		params.Scores = append(params.Scores, score.score)
	}
	for i, result := range results {
		if result.Err != nil || result.NPC {
			continue
		}
		flag := b.flags[b.submission.Values[i]]
		if flag == nil {
			return sqlc.ApplySubmissionChangesParams{}, fmt.Errorf("%s - find accepted flag: %w", location, domain.ErrFlagNotFound)
		}
		params.FlagIds = append(params.FlagIds, flag.row.ID)
		params.Deltas = append(params.Deltas, result.Attack.AttackerDelta)
	}
	if len(params.FlagIds) != len(params.Deltas) {
		return sqlc.ApplySubmissionChangesParams{}, fmt.Errorf("%s - pair accepted flags with deltas: lengths differ", location)
	}
	return params, nil
}

func (b *attackBatch) saveChanges(ctx context.Context, queries *sqlc.Queries, results []domain.SubmissionResult) error {
	const location = "postgres.attackBatch.saveChanges"
	params, err := b.changeParams(results)
	if err != nil {
		return fmt.Errorf("%s - prepare score and event changes: %w", location, err)
	}
	if len(params.FlagIds) == 0 && len(params.TeamIds) == 0 {
		return nil
	}
	// Both writes use the already locked rows and are independent. One query
	// saves a network round trip while keeping both affected-row checks before COMMIT.
	rows, err := queries.ApplySubmissionChanges(ctx, params)
	if err != nil {
		return fmt.Errorf("%s - update scores and accepted flag events: %w", location, err)
	}
	if rows.ScoreRows != int64(len(params.TeamIds)) {
		return fmt.Errorf("%s - update all affected scores: %w", location, domain.ErrTeamTaskMissing)
	}
	if rows.EventRows != int64(len(params.FlagIds)) {
		return fmt.Errorf("%s - update all accepted flag events: %w", location, domain.ErrNotFound)
	}
	return nil
}

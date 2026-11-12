package postgres

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func predictionFixture() *attackBatch {
	attacker, victim, task := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	flags := map[string]*attackFlag{}
	for _, value := range []string{"second", "first"} {
		flags[value] = &attackFlag{
			row: sqlc.ReadSubmissionFlagsRow{
				ID:     uuid.NewV7(),
				TeamID: victim,
				TaskID: task,
			},
		}
	}
	return &attackBatch{
		submission: domain.FlagSubmission{
			TeamID:     attacker,
			Values:     []string{"first", "second", "first", "unknown"},
			ReceivedAt: time.Unix(100, 0),
			Generation: 3,
		},
		state: submissionState{
			game: sqlc.LockGameForAttackRow{
				GameHardness: 10,
				Inflation:    true,
			},
			generation: 3,
		},
		flags: flags,
		scores: map[teamTaskKey]*attackScore{
			{attacker, task}: {
				status: domain.TaskStatusUp,
				score:  100,
			},
			{victim, task}: {
				status: domain.TaskStatusUp,
				score:  900,
			},
		},
	}
}

func TestPredictChangesPreservesInputOrderAndOriginalScores(t *testing.T) {
	b := predictionFixture()
	prediction, ok := b.predictChanges()
	if !ok {
		t.Fatal("prediction rejected a healthy batch")
	}
	first, err := domain.CalculateRatingChange(100, 900, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.CalculateRatingChange(100+first.Attacker, 900+first.Victim, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if prediction.results[0].Attack.AttackerDelta != first.Attacker || prediction.results[1].Attack.AttackerDelta != second.Attacker ||
		!errors.Is(prediction.results[2].Err, domain.ErrFlagAlreadyStolen) || !errors.Is(prediction.results[3].Err, domain.ErrFlagNotFound) {
		t.Fatalf("prediction changed input ordering or duplicate verdicts: %+v", prediction.results)
	}
	if !reflect.DeepEqual(prediction.changes.FlagIds, []uuid.UUID{b.flags["first"].row.ID, b.flags["second"].row.ID}) ||
		!reflect.DeepEqual(prediction.changes.Deltas, []float64{first.Attacker, second.Attacker}) {
		t.Fatalf("prediction mismatched IDs/deltas: %+v", prediction.changes)
	}
	for key, score := range b.scores {
		if score.stolen != 0 || score.lost != 0 || score == prediction.batch.scores[key] {
			t.Fatal("prediction mutated or aliased original scores")
		}
		want := 900.0
		if key.team == b.submission.TeamID {
			want = 100
		}
		if score.score != want {
			t.Fatal("prediction changed original score")
		}
	}
}

func TestPredictionRejectsNPCAndFallbackSkipsNPCScoring(t *testing.T) {
	b := predictionFixture()
	npc := b.flags["first"]
	npc.row.TeamID = uuid.NewV7()
	npc.row.Kind = domain.TeamKindNPC

	if _, ok := b.predictChanges(); ok {
		t.Fatal("prediction fast path accepted a batch containing an NPC flag")
	}
	for _, score := range b.scores {
		if score.stolen != 0 || score.lost != 0 {
			t.Fatal("rejected prediction mutated counters")
		}
	}

	inserted := map[uuid.UUID]bool{
		npc.row.ID:               true,
		b.flags["second"].row.ID: true,
	}
	results, err := b.scoreResults(inserted)
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].NPC || results[0].Err != nil {
		t.Fatalf("NPC result = %+v, want successful NPC acceptance", results[0])
	}
	if results[1].NPC || results[1].Err != nil {
		t.Fatalf("player result = %+v, want ordinary acceptance", results[1])
	}
	if !errors.Is(results[2].Err, domain.ErrFlagAlreadyStolen) {
		t.Fatalf("repeated NPC result = %+v, want duplicate", results[2])
	}
	changes, err := b.changeParams(results)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.TeamIds) != 2 ||
		!reflect.DeepEqual(changes.FlagIds, []uuid.UUID{b.flags["second"].row.ID}) ||
		len(changes.Deltas) != 1 {
		t.Fatalf("score changes = %+v, want only the player attack", changes)
	}
	if _, exists := b.scores[teamTaskKey{npc.row.TeamID, npc.row.TaskID}]; exists {
		t.Fatal("fallback created a score row for the NPC")
	}
}

func TestPartialPredictionRecomputesOnlyActualInsert(t *testing.T) {
	b := predictionFixture()
	prediction, ok := b.predictChanges()
	if !ok {
		t.Fatal("prediction failed")
	}
	id := b.flags["second"].row.ID
	db := &predictionDB{
		ids: []uuid.UUID{id},
	}
	results, err := b.applyPredicted(t.Context(), sqlc.New(db), prediction)
	if err != nil {
		t.Fatal(err)
	}
	first, err := domain.CalculateRatingChange(100, 900, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(results[0].Err, domain.ErrFlagAlreadyStolen) || results[1].Err != nil || results[1].Attack.AttackerDelta != first.Attacker {
		t.Fatalf("fallback used speculative scores or accepted conflict: %+v", results)
	}
	if db.rowCalls != 2 || len(db.applyArgs) != 8 || !reflect.DeepEqual(db.applyArgs[5], []uuid.UUID{id}) ||
		!reflect.DeepEqual(db.applyArgs[6], []float64{first.Attacker}) {
		t.Fatalf("fallback did not correct only actual inserted event: calls=%d args=%#v", db.rowCalls, db.applyArgs)
	}
}

func TestPredictedZeroInsertSkipsScoreAndEventWrites(t *testing.T) {
	b := predictionFixture()
	prediction, ok := b.predictChanges()
	if !ok {
		t.Fatal("prediction failed")
	}
	db := &predictionDB{}
	results, err := b.applyPredicted(t.Context(), sqlc.New(db), prediction)
	if err != nil || db.rowCalls != 1 || db.queryCalls != 0 {
		t.Fatalf("zero insert performed another write: %+v, %v, %+v", results, err, db)
	}
	for _, result := range results[:3] {
		if !errors.Is(result.Err, domain.ErrFlagAlreadyStolen) {
			t.Fatalf("zero insert accepted a flag: %+v", results)
		}
	}
}

func TestPredictionInvariantFailuresDoNotFallback(t *testing.T) {
	for _, name := range []string{"SQL error", "unexpected ID", "duplicate ID", "full insert missing score", "partial insert changed score"} {
		t.Run(name, func(t *testing.T) {
			b := predictionFixture()
			prediction, ok := b.predictChanges()
			if !ok {
				t.Fatal("prediction failed")
			}
			first := b.flags["first"].row.ID
			db := &predictionDB{}
			switch name {
			case "SQL error":
				db.err = context.Canceled
			case "unexpected ID":
				db.ids = []uuid.UUID{uuid.NewV7()}
			case "duplicate ID":
				db.ids = []uuid.UUID{first, first}
			case "full insert missing score":
				db.ids = prediction.changes.FlagIds
				db.scoreRows = 1
			case "partial insert changed score":
				db.ids = []uuid.UUID{first}
				db.scoreRows = 1
			}
			results, err := b.applyPredicted(t.Context(), sqlc.New(db), prediction)
			if err == nil || results != nil || db.rowCalls != 1 || db.queryCalls != 0 {
				t.Fatalf("invariant failure fell back or accepted flags: %+v, %v, %+v", results, err, db)
			}
		})
	}
}

func TestSpeculativeRatingFailureUsesOriginalInsertPath(t *testing.T) {
	b := predictionFixture()
	badVictim := uuid.NewV7()
	b.submission.Values = []string{"second", "first", "second", "unknown"}
	b.flags["first"].row.TeamID = badVictim
	b.scores[teamTaskKey{badVictim, b.flags["first"].row.TaskID}] = &attackScore{
		score: math.NaN(),
	}
	if _, ok := b.predictChanges(); ok {
		t.Fatal("invalid speculative rating was accepted")
	}
	for _, score := range b.scores {
		if score.stolen != 0 || score.lost != 0 {
			t.Fatal("failed prediction mutated original scores")
		}
	}
	if b.scores[teamTaskKey{b.submission.TeamID, b.flags["second"].row.TaskID}].score != 100 {
		t.Fatal("failed prediction mutated attacker before the invalid candidate")
	}
	db := &predictionDB{
		ids: []uuid.UUID{b.flags["second"].row.ID},
	}
	results, err := b.apply(t.Context(), sqlc.New(db))
	if err != nil || db.queryCalls != 1 || db.rowCalls != 1 {
		t.Fatalf("speculative failure rejected an already-stolen invalid candidate: %+v %v %+v", results, err, db)
	}
	if results[0].Err != nil || !errors.Is(results[1].Err, domain.ErrFlagAlreadyStolen) {
		t.Fatalf("original path produced wrong verdicts: %+v", results)
	}
}

func TestCheckedInsertedFlagsRejectsDuplicateExpectedIDs(t *testing.T) {
	id := uuid.NewV7()
	if _, err := checkedInsertedFlags([]uuid.UUID{id, id}, []uuid.UUID{id}); err == nil {
		t.Fatal("duplicate expected IDs were accepted")
	}
}

type predictionDB struct {
	ids                  []uuid.UUID
	scoreRows            int64
	err                  error
	rowCalls, queryCalls int
	applyArgs            []any
}

func (*predictionDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}
func (db *predictionDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	db.queryCalls++
	return &predictionRows{
		ids: db.ids,
	}, nil
}
func (db *predictionDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.rowCalls++
	if strings.Contains(query, "-- name: ApplySubmissionChanges") {
		db.applyArgs = args
		return predictionRow{
			scan: func(dest []any) {
				*dest[0].(*int64) = int64(len(args[0].([]uuid.UUID)))
				*dest[1].(*int64) = int64(len(args[5].([]uuid.UUID)))
			},
		}
	}
	return predictionRow{
		err: db.err,
		scan: func(dest []any) {
			*dest[0].(*[]uuid.UUID) = db.ids
			*dest[1].(*int64) = db.scoreRows
		},
	}
}

type predictionRow struct {
	err  error
	scan func([]any)
}

func (r predictionRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	r.scan(dest)
	return nil
}

type predictionRows struct {
	emptyRows

	ids  []uuid.UUID
	next int
}

func (r *predictionRows) Next() bool {
	r.next++
	return r.next <= len(r.ids)
}
func (r *predictionRows) Scan(dest ...any) error {
	*dest[0].(*uuid.UUID) = r.ids[r.next-1]
	return nil
}

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type submissionFixture struct {
	pool       *pgxpool.Pool
	repo       *postgres.Repository
	teams      []uuid.UUID
	task       uuid.UUID
	generation int64
}

func newSubmissionFixture(t *testing.T) submissionFixture {
	t.Helper()
	pool, _ := testutil.Database(t)
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(t.Context(), gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(t.Context(), t, game)
	if _, err := pool.Exec(t.Context(), `UPDATE GameConfig SET game_running=TRUE, real_round=1, flag_lifetime=2, inflation=FALSE`); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return submissionFixture{pool, postgres.New(pool), teams, tasks[0], life.Session.Generation}
}

func (f submissionFixture) flags(
	t *testing.T,
	victim int,
	start, count int,
) []string {
	t.Helper()
	values := make([]string, count)
	for i := range values {
		values[i] = fmt.Sprintf("%031d=", start+i)
		if _, err := f.repo.Create(t.Context(), domain.Flag{
			Value:  values[i],
			TeamID: f.teams[victim],
			TaskID: f.task,
			Round:  1,
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return values
}

func (f *submissionFixture) addTeam(t *testing.T, name, kind string) int {
	t.Helper()
	id := uuid.NewV7()
	index := len(f.teams)
	if _, err := f.pool.Exec(t.Context(), `
		INSERT INTO Teams (id, name, ip, token, kind)
		VALUES ($1, $2, $3, $4, $5)`,
		id, name, fmt.Sprintf("127.0.0.%d", index+1), fmt.Sprintf("%016x", index+1), kind,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `
		INSERT INTO TeamTasks (team_id, task_id, status, score)
		VALUES ($1, $2, $3, 2500)`,
		id, f.task, domain.TaskStatusUp,
	); err != nil {
		t.Fatal(err)
	}
	f.teams = append(f.teams, id)
	return index
}

func (f submissionFixture) request(team int, values []string) domain.FlagSubmission {
	return domain.FlagSubmission{
		TeamID:     f.teams[team],
		Values:     values,
		Generation: f.generation,
		ReceivedAt: time.Now(),
	}
}

func TestBatchPreservesSequentialScoringAndVerdictOrder(t *testing.T) {
	f := newSubmissionFixture(t)
	values := f.flags(t, 1, 1, 98)
	values = append(values, values[0], "unknown")
	results, err := f.repo.SubmitBatch(t.Context(), f.request(0, values))
	if err != nil {
		t.Fatal(err)
	}
	attacker, victim := 2500.0, 2500.0
	for i, result := range results[:98] {
		change, err := domain.CalculateRatingChange(attacker, victim, 10, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Err != nil || math.Abs(result.Attack.AttackerDelta-change.Attacker) > 1e-10 {
			t.Fatalf("flag %d: %+v, expected %v", i, result, change)
		}
		attacker += change.Attacker
		victim += change.Victim
	}
	if !errors.Is(results[98].Err, domain.ErrFlagAlreadyStolen) || !errors.Is(results[99].Err, domain.ErrFlagNotFound) {
		t.Fatalf("verdict order: %+v", results[98:])
	}
	var score float64
	var stolen, lost, count int
	if err := f.pool.QueryRow(t.Context(), `SELECT score, stolen, (SELECT sum(lost) FROM TeamTasks), (SELECT count(*) FROM StolenFlags) FROM TeamTasks WHERE team_id=$1 AND task_id=$2`, f.teams[0], f.task).Scan(&score, &stolen, &lost, &count); err != nil {
		t.Fatal(err)
	}
	if score != attacker || stolen != 98 || lost != 98 || count != 98 {
		t.Fatalf("score=%v expected=%v stolen=%d lost=%d records=%d", score, attacker, stolen, lost, count)
	}
}

func TestNPCFlagsPersistOnceWithoutScoresOrCounters(t *testing.T) {
	f := newSubmissionFixture(t)
	if _, err := f.pool.Exec(t.Context(), `UPDATE Teams SET kind=$1 WHERE id=$2`, domain.TeamKindNPC, f.teams[1]); err != nil {
		t.Fatal(err)
	}
	player := f.addTeam(t, "third", domain.TeamKindPlayer)
	npcFlags := f.flags(t, 1, 1001, 2)
	playerFlag := f.flags(t, player, 2001, 1)[0]

	before := teamTaskSnapshot(t, f)
	first, err := f.repo.SubmitBatch(t.Context(), f.request(0, npcFlags[:1]))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || !first[0].NPC || first[0].Err != nil || first[0].Attack != (domain.AttackResult{}) {
		t.Fatalf("first NPC result = %+v", first)
	}
	if after := teamTaskSnapshot(t, f); after != before {
		t.Fatalf("NPC submission changed team scores or counters: before=%s after=%s", before, after)
	}

	duplicate, err := f.repo.SubmitBatch(t.Context(), f.request(0, npcFlags[:1]))
	if err != nil {
		t.Fatal(err)
	}
	if len(duplicate) != 1 || !errors.Is(duplicate[0].Err, domain.ErrFlagAlreadyStolen) || duplicate[0].NPC {
		t.Fatalf("duplicate NPC result = %+v", duplicate)
	}

	mixed, err := f.repo.SubmitBatch(t.Context(), f.request(0, []string{npcFlags[1], playerFlag, npcFlags[1]}))
	if err != nil {
		t.Fatal(err)
	}
	if len(mixed) != 3 || !mixed[0].NPC || mixed[0].Err != nil || mixed[1].NPC || mixed[1].Err != nil ||
		!errors.Is(mixed[2].Err, domain.ErrFlagAlreadyStolen) {
		t.Fatalf("mixed results = %+v", mixed)
	}
	var npcScore float64
	var npcStolen, npcLost, attackerStolen, records int
	if err := f.pool.QueryRow(t.Context(), `
		SELECT npc.score, npc.stolen, npc.lost, attacker.stolen,
		       (SELECT count(*) FROM StolenFlags)
		FROM TeamTasks AS npc
		JOIN TeamTasks AS attacker ON attacker.team_id=$1 AND attacker.task_id=npc.task_id
		WHERE npc.team_id=$2 AND npc.task_id=$3`,
		f.teams[0], f.teams[1], f.task,
	).Scan(&npcScore, &npcStolen, &npcLost, &attackerStolen, &records); err != nil {
		t.Fatal(err)
	}
	if npcScore != 2500 || npcStolen != 0 || npcLost != 0 || attackerStolen != 1 || records != 3 {
		t.Fatalf("NPC/player accounting: npc=%v/%d/%d attacker_stolen=%d records=%d", npcScore, npcStolen, npcLost, attackerStolen, records)
	}
	beforeOther := teamTaskSnapshot(t, f)
	other, err := f.repo.SubmitBatch(t.Context(), f.request(player, npcFlags[:1]))
	if err != nil || len(other) != 1 || !other[0].NPC || other[0].Err != nil {
		t.Fatalf("another attacker submitting same NPC flag: result=%+v err=%v", other, err)
	}
	if afterOther := teamTaskSnapshot(t, f); afterOther != beforeOther {
		t.Fatalf("another attacker's NPC capture changed scores: before=%s after=%s", beforeOther, afterOther)
	}
}

func teamTaskSnapshot(t *testing.T, f submissionFixture) string {
	t.Helper()
	var snapshot string
	if err := f.pool.QueryRow(t.Context(), `
		SELECT COALESCE(jsonb_agg(
			jsonb_build_array(team_id, task_id, score, stolen, lost)
			ORDER BY team_id, task_id
		), '[]'::JSONB)::TEXT
		FROM TeamTasks`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestReceivedBatchSurvivesRoundAdvanceAndExpiredRetry(t *testing.T) {
	f := newSubmissionFixture(t)
	values := f.flags(t, 1, 1, 2)
	received := time.Now().Add(-time.Second)
	ctx := t.Context()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if _, err = tx.Exec(ctx, `UPDATE GameConfig SET real_round=4`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO DispatchRuns(schedule_id,round,is_round,round_start,completed) VALUES ('classic_rounds',3,TRUE,$1,TRUE),('classic_rounds',4,TRUE,$2,TRUE)`, float64(received.Add(-time.Second).UnixMicro())/1e6, float64(received.Add(time.Millisecond).UnixMicro())/1e6); err != nil {
		t.Fatal(err)
	}
	request := f.request(0, values[:1])
	request.ReceivedAt = received
	done := make(chan error, 1)
	go func() {
		results, err := f.repo.SubmitBatch(ctx, request)
		if err == nil {
			err = results[0].Err
		}
		done <- err
	}()
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("timely batch expired while waiting: %v", err)
	}
	results, err := f.repo.SubmitBatch(ctx, f.request(0, values))
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(results[0].Err, domain.ErrFlagAlreadyStolen) || !errors.Is(results[1].Err, domain.ErrFlagTooOld) {
		t.Fatalf("expired retry: %+v", results)
	}
	var submitted time.Time
	if err := f.pool.QueryRow(ctx, `SELECT submit_time FROM StolenFlags`).Scan(&submitted); err != nil {
		t.Fatal(err)
	}
	if submitted.Sub(received).Abs() >= time.Microsecond {
		t.Fatalf("receipt timestamp changed: %v vs %v", submitted, received)
	}
}

func TestBatchRollbackLeavesNoAcceptedFlagsOrScoreChanges(t *testing.T) {
	f := newSubmissionFixture(t)
	values := f.flags(t, 1, 1, 20)
	_, err := f.pool.Exec(t.Context(), `CREATE FUNCTION reject_score() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'score write unavailable'; END $$;
CREATE TRIGGER reject_score BEFORE UPDATE ON TeamTasks FOR EACH ROW EXECUTE FUNCTION reject_score()`)
	if err != nil {
		t.Fatal(err)
	}
	results, err := f.repo.SubmitBatch(t.Context(), f.request(0, values))
	if err == nil || results != nil {
		t.Fatalf("storage failure produced verdicts: %+v %v", results, err)
	}
	var count, stolen int
	var score float64
	if err := f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM StolenFlags),sum(stolen),sum(score) FROM TeamTasks`).Scan(&count, &stolen, &score); err != nil {
		t.Fatal(err)
	}
	if count != 0 || stolen != 0 || score != 5000 {
		t.Fatalf("partial commit: records=%d stolen=%d score=%v", count, stolen, score)
	}
}

func TestOverlappingOpposingBatchesAwardEachFlagOnce(t *testing.T) {
	f := newSubmissionFixture(t)
	requests := []domain.FlagSubmission{f.request(0, f.flags(t, 1, 1, 100)), f.request(1, f.flags(t, 0, 101, 100))}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range 20 {
		workers.Go(func() {
			<-start
			results, err := f.repo.SubmitBatch(ctx, requests[i%2])
			if err != nil {
				t.Error(err)
				return
			}
			for _, result := range results {
				if result.Err != nil && !errors.Is(result.Err, domain.ErrFlagAlreadyStolen) {
					t.Error(result.Err)
				}
			}
		})
	}
	close(start)
	workers.Wait()
	var count, stolen, lost int
	var score float64
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM StolenFlags),sum(stolen),sum(lost),sum(score) FROM TeamTasks`).Scan(&count, &stolen, &lost, &score); err != nil {
		t.Fatal(err)
	}
	if count != 200 || stolen != 200 || lost != 200 || math.Abs(score-5000) > 1e-8 {
		t.Fatalf("batch conservation: records=%d stolen=%d lost=%d score=%v", count, stolen, lost, score)
	}
}

func TestBatchRejectsGenerationChange(t *testing.T) {
	f := newSubmissionFixture(t)
	request := f.request(0, f.flags(t, 1, 1, 2))
	if _, err := f.pool.Exec(t.Context(), `UPDATE GameSession SET generation=generation+1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.SubmitBatch(t.Context(), request); !errors.Is(err, domain.ErrGameUnavailable) {
		t.Fatalf("old generation accepted: %v", err)
	}
}

func TestVolgaBatchChecksAttackerStatusWithoutDroppingOtherVerdicts(t *testing.T) {
	f := newSubmissionFixture(t)
	foreign := f.flags(t, 1, 1, 1)
	own := f.flags(t, 0, 2, 1)
	request := f.request(0, []string{foreign[0], own[0], "unknown"})
	if _, err := f.pool.Exec(t.Context(), `UPDATE GameConfig SET volga_attacks_mode=TRUE`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE TeamTasks SET status=104 WHERE team_id=$1`, f.teams[0]); err != nil {
		t.Fatal(err)
	}
	results, err := f.repo.SubmitBatch(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []error{domain.ErrServiceDown, domain.ErrOwnFlag, domain.ErrFlagNotFound} {
		if !errors.Is(results[i].Err, want) {
			t.Fatalf("verdict %d: %v, want %v", i, results[i].Err, want)
		}
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE TeamTasks SET status=101 WHERE team_id=$1`, f.teams[0]); err != nil {
		t.Fatal(err)
	}
	results, err = f.repo.SubmitBatch(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Err != nil {
		t.Fatalf("recovered attacker cannot submit: %v", results[0].Err)
	}
	var count int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM StolenFlags`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one accepted flag after recovery, got %d", count)
	}
}

func TestCanceledBatchDoesNotCommit(t *testing.T) {
	f := newSubmissionFixture(t)
	request := f.request(0, f.flags(t, 1, 1, 10))
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()
	if _, err := tx.Exec(t.Context(), `SELECT 1 FROM TeamTasks FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	results, err := f.repo.SubmitBatch(ctx, request)
	if err == nil || results != nil {
		t.Fatalf("canceled batch returned verdicts: %+v %v", results, err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM StolenFlags`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canceled batch recorded %d flags", count)
	}
}

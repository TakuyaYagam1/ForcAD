package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestConcurrentFlagSubmissionsAwardOnce(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	if _, err := pool.Exec(ctx, `UPDATE GameConfig SET game_running=TRUE,real_round=1,inflation=FALSE`); err != nil {
		t.Fatal(err)
	}
	repo := postgres.New(pool)
	flag, err := repo.Create(ctx, domain.Flag{Value: strings.Repeat("A", 31) + "=", TeamID: teams[1], TaskID: tasks[0], Round: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 64)
	var workers sync.WaitGroup
	for range 64 {
		workers.Go(func() { <-start; err := submitFlag(ctx, repo, game, teams[0], flag); results <- err })
	}
	close(start)
	workers.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, domain.ErrFlagAlreadyStolen) {
			t.Errorf("concurrent submit: %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d copies", accepted)
	}
	var attacker, victim float64
	var stolen, lost int
	if err := pool.QueryRow(ctx, `SELECT score,stolen FROM TeamTasks WHERE team_id=$1 AND task_id=$2`, teams[0], tasks[0]).Scan(&attacker, &stolen); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT score,lost FROM TeamTasks WHERE team_id=$1 AND task_id=$2`, teams[1], tasks[0]).Scan(&victim, &lost); err != nil {
		t.Fatal(err)
	}
	expected := 25 * math.Sqrt(10)
	if stolen != 1 || lost != 1 || math.Abs(attacker-2500-expected) > 1e-8 || math.Abs(victim-2500+expected) > 1e-8 {
		t.Fatalf("non-atomic score: attacker=%v victim=%v stolen=%d lost=%d", attacker, victim, stolen, lost)
	}
}

func TestOpposingSubmissionsPreserveScoresWithoutDeadlock(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	if _, err := pool.Exec(ctx, `UPDATE GameConfig SET game_running=TRUE,real_round=1,inflation=FALSE`); err != nil {
		t.Fatal(err)
	}
	repo := postgres.New(pool)
	type submission struct {
		attacker uuid.UUID
		flag     domain.Flag
	}
	jobs := make(chan submission, 100)
	for i := range 100 {
		victimIndex := i % 2
		flag, err := repo.Create(ctx, domain.Flag{Value: fmt.Sprintf("%031d=", i), TeamID: teams[victimIndex], TaskID: tasks[0], Round: 1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		jobs <- submission{attacker: teams[1-victimIndex], flag: flag}
	}
	close(jobs)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for job := range jobs {
				if err := submitFlag(ctx, repo, game, job.attacker, job.flag); err != nil {
					t.Errorf("opposing submit: %v", err)
				}
			}
		})
	}
	workers.Wait()
	var total float64
	var stolen, lost, records int
	if err := pool.QueryRow(ctx, `SELECT sum(score),sum(stolen),sum(lost),(SELECT count(*) FROM StolenFlags) FROM TeamTasks`).Scan(&total, &stolen, &lost, &records); err != nil {
		t.Fatal(err)
	}
	if math.Abs(total-5000) > 1e-8 || stolen != 100 || lost != 100 || records != 100 {
		t.Fatalf("score conservation: total=%v stolen=%d lost=%d records=%d", total, stolen, lost, records)
	}
}

func TestSubmissionRechecksGameStateInsideTransaction(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	if _, err := pool.Exec(ctx, `UPDATE GameConfig SET game_running=TRUE,real_round=1`); err != nil {
		t.Fatal(err)
	}
	repo := postgres.New(pool)
	flag, err := repo.Create(ctx, domain.Flag{Value: strings.Repeat("B", 31) + "=", TeamID: teams[1], TaskID: tasks[0], Round: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = game.ChangeState(ctx, "pause", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = submitFlag(ctx, repo, game, teams[0], flag); !errors.Is(err, domain.ErrGameUnavailable) {
		t.Fatalf("paused game submit: %v", err)
	}
	if err = game.ChangeState(ctx, "resume", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE GameConfig SET real_round=5`); err != nil {
		t.Fatal(err)
	}
	if err = submitFlag(ctx, repo, game, teams[0], flag); !errors.Is(err, domain.ErrFlagTooOld) {
		t.Fatalf("stale flag submit: %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM StolenFlags`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected flags changed scoring: %d", count)
	}
}

func TestAttackDataFiltersWithUUIDTaskArray(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	repository := postgres.New(pool)
	if _, err := repository.Create(ctx, domain.Flag{
		Value: "UUID-ARRAY-FLAG", TeamID: teams[0], TaskID: tasks[0], Round: 1,
		PublicData: "public array data", PrivateData: "private array data",
	}, nil); err != nil {
		t.Fatal(err)
	}

	data, err := repository.AttackData(ctx, 1, []uuid.UUID{tasks[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 || data[0].TaskID != tasks[0] || data[0].PublicData != "public array data" {
		t.Fatalf("attack data for UUID task array = %+v", data)
	}
}

func submitFlag(ctx context.Context, repo *postgres.Repository, game *postgres.GameStore, team uuid.UUID, flag domain.Flag) error {
	life, err := game.Lifecycle(ctx)
	if err != nil {
		return err
	}
	results, err := repo.SubmitBatch(ctx, domain.FlagSubmission{TeamID: team, Values: []string{flag.Value}, ReceivedAt: time.Now(), Generation: life.Session.Generation})
	if err != nil {
		return err
	}
	return results[0].Err
}

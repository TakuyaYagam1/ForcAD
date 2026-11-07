package usecase

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type attackFlightRepository struct {
	outbound.GameRepository

	mu             sync.Mutex
	life           domain.Lifecycle
	tasks          []domain.Task
	lifecycleCalls atomic.Int64
	taskCalls      atomic.Int64
}

func (r *attackFlightRepository) Lifecycle(context.Context) (domain.Lifecycle, error) {
	r.lifecycleCalls.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.life, nil
}

func (r *attackFlightRepository) Tasks(context.Context, bool) ([]domain.Task, error) {
	r.taskCalls.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Task(nil), r.tasks...), nil
}

type attackFlightQuery struct {
	ctx     context.Context
	round   int32
	ids     []uuid.UUID
	release chan struct{}
	err     error
}

type attackFlightFlags struct {
	outbound.FlagRepository

	calls   atomic.Int64
	entered chan *attackFlightQuery
	rows    []domain.AttackDatum
}

func (f *attackFlightFlags) AttackData(ctx context.Context, round int32, ids []uuid.UUID) ([]domain.AttackDatum, error) {
	count := f.calls.Add(1)
	query := &attackFlightQuery{
		ctx:     ctx,
		round:   round,
		ids:     append([]uuid.UUID(nil), ids...),
		release: make(chan struct{}),
	}
	f.entered <- query
	select {
	case <-query.release:
		if query.err != nil {
			return nil, query.err
		}
		if f.rows != nil {
			return append([]domain.AttackDatum(nil), f.rows...), nil
		}
		rows := make([]domain.AttackDatum, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, domain.AttackDatum{
				TaskID:     id,
				TeamIP:     "10.0.0.1",
				PublicData: strconv.FormatInt(count, 10),
			})
		}
		return rows, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type attackFlightResult struct {
	data map[string]map[string][]string
	err  error
}

func newAttackFlightGame(t *testing.T) (*Game, *attackFlightRepository, *attackFlightFlags) {
	t.Helper()
	repo := &attackFlightRepository{
		life: domain.Lifecycle{
			Config: domain.GameConfig{
				RealRound:    7,
				FlagLifetime: 3,
			},
			Session: domain.Session{
				Generation: 4,
			},
		},
		tasks: []domain.Task{
			{
				ID:          uuid.MustParse("00000000-0000-7000-8000-000000000002"),
				Name:        "second",
				CheckerType: "pfr",
			},
			{
				ID:          uuid.MustParse("00000000-0000-7000-8000-000000000001"),
				Name:        "first",
				CheckerType: "pfr",
			},
		},
	}
	flags := &attackFlightFlags{
		entered: make(chan *attackFlightQuery, 32),
	}
	service := NewGame(repo, flags, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(service.Close)
	return service, repo, flags
}

func startAttackFlight(ctx context.Context, service *Game, filter *string) <-chan attackFlightResult {
	result := make(chan attackFlightResult, 1)
	go func() {
		data, err := service.AttackData(ctx, filter)
		result <- attackFlightResult{
			data: data,
			err:  err,
		}
	}()
	return result
}

func joinedAttackFlight(ctx context.Context, service *Game) <-chan attackFlightResult {
	observed := &runtimeObservedContext{
		Context: ctx,
		ready:   make(chan struct{}),
	}
	//nolint:contextcheck // The observing wrapper embeds the caller context and forwards its cancellation.
	result := startAttackFlight(observed, service, nil)
	<-observed.ready
	// Done also observes the shared lifecycle wait now; let this caller finish
	// metadata reads and block on the heavy attack-data query before returning.
	synctest.Wait()
	return result
}

func TestAttackDataCoalescesOnlyHeavyQueryAndReturnsIndependentMaps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, repo, flags := newAttackFlightGame(t)
		leader := startAttackFlight(t.Context(), service, nil)
		query := <-flags.entered
		results := make([]<-chan attackFlightResult, 0, 13)
		results = append(results, leader)
		for range 12 {
			results = append(results, joinedAttackFlight(t.Context(), service))
		}
		if flags.calls.Load() != 1 || repo.lifecycleCalls.Load() != 13 || repo.taskCalls.Load() != 13 {
			t.Fatalf("unexpected reads: flags=%d lifecycle=%d tasks=%d", flags.calls.Load(), repo.lifecycleCalls.Load(), repo.taskCalls.Load())
		}
		if len(query.ids) != 2 || query.ids[0].Compare(query.ids[1]) >= 0 || query.round != 7 {
			t.Fatalf("query scope was not canonical: %+v", query)
		}
		close(query.release)
		for i, result := range results {
			got := <-result
			if got.err != nil || got.data["first"]["10.0.0.1"][0] != "1" {
				t.Fatalf("caller %d: %+v", i, got)
			}
			got.data["first"]["10.0.0.1"][0] = "caller mutation"
			delete(got.data, "second")
		}
		// A finished flight is never reused, even in the same round.
		later := startAttackFlight(t.Context(), service, nil)
		next := <-flags.entered
		close(next.release)
		got := <-later
		if got.err != nil || got.data["first"]["10.0.0.1"][0] != "2" || flags.calls.Load() != 2 {
			t.Fatalf("completed query was cached: %+v calls=%d", got, flags.calls.Load())
		}
	})
}

func TestAttackDataCallerCancellationDoesNotCancelSharedQuery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, _, flags := newAttackFlightGame(t)
		leaderCtx, cancelLeader := context.WithCancel(t.Context())
		defer cancelLeader()
		leader := startAttackFlight(leaderCtx, service, nil)
		query := <-flags.entered
		waiterCtx, cancelWaiter := context.WithCancel(t.Context())
		defer cancelWaiter()
		waiter := joinedAttackFlight(waiterCtx, service)
		survivor := joinedAttackFlight(t.Context(), service)
		cancelLeader()
		cancelWaiter()
		if got := <-leader; !errors.Is(got.err, context.Canceled) {
			t.Fatalf("leader cancellation lost: %v", got.err)
		}
		if got := <-waiter; !errors.Is(got.err, context.Canceled) {
			t.Fatalf("waiter cancellation lost: %v", got.err)
		}
		if err := query.ctx.Err(); err != nil {
			t.Fatalf("caller canceled shared query: %v", err)
		}
		close(query.release)
		if got := <-survivor; got.err != nil || len(got.data) != 2 {
			t.Fatalf("surviving waiter failed: %+v", got)
		}
		if flags.calls.Load() != 1 {
			t.Fatalf("shared query count=%d, want 1", flags.calls.Load())
		}
	})
}

func TestAttackDataFlightKeySeparatesGameAndTaskScopes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, repo, flags := newAttackFlightGame(t)
		baseline := startAttackFlight(t.Context(), service, nil)
		queries := make([]*attackFlightQuery, 0, 6)
		queries = append(queries, <-flags.entered)
		results := make([]<-chan attackFlightResult, 0, 7)
		results = append(results, baseline)
		repo.mu.Lock()
		repo.tasks[0], repo.tasks[1] = repo.tasks[1], repo.tasks[0]
		repo.mu.Unlock()
		// Task order is immaterial; a matching set joins the blocked query.
		results = append(results, joinedAttackFlight(t.Context(), service))
		if flags.calls.Load() != 1 {
			t.Fatal("task order created a distinct flight")
		}
		initial := repo.life
		for _, change := range []func(*domain.Lifecycle){
			func(life *domain.Lifecycle) {
				life.Session.Generation++
			},
			func(life *domain.Lifecycle) {
				life.Config.RealRound++
			},
			func(life *domain.Lifecycle) {
				life.Config.FlagLifetime++
			},
		} {
			repo.mu.Lock()
			repo.life = initial
			change(&repo.life)
			repo.mu.Unlock()
			results = append(results, startAttackFlight(t.Context(), service, nil))
			queries = append(queries, <-flags.entered)
		}
		repo.mu.Lock()
		repo.life = initial
		repo.mu.Unlock()
		for _, filter := range []string{"first", "second"} {
			results = append(results, startAttackFlight(t.Context(), service, &filter))
			query := <-flags.entered
			if len(query.ids) != 1 {
				t.Fatalf("filtered query included other service: %+v", query.ids)
			}
			queries = append(queries, query)
		}
		if flags.calls.Load() != 6 {
			t.Fatalf("independent scope count=%d, want 6", flags.calls.Load())
		}
		for _, query := range queries {
			close(query.release)
		}
		for _, result := range results {
			if got := <-result; got.err != nil {
				t.Fatal(got.err)
			}
		}
	})
}

func TestAttackDataFastRejectsCanceledCallerAndClosedGame(t *testing.T) {
	service, repo, flags := newAttackFlightGame(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.AttackData(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("already canceled caller: %v", err)
	}
	service.Close()
	if _, err := service.AttackData(t.Context(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("already closed game: %v", err)
	}
	if repo.lifecycleCalls.Load() != 0 || repo.taskCalls.Load() != 0 || flags.calls.Load() != 0 {
		t.Fatal("canceled request reached storage")
	}
}

func TestAttackDataSharedQueryLifetime(t *testing.T) {
	for _, closeGame := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			service, _, flags := newAttackFlightGame(t)
			result := startAttackFlight(t.Context(), service, nil)
			query := <-flags.entered
			deadline, ok := query.ctx.Deadline()
			if !ok || time.Until(deadline) != attackDataLoadTimeout {
				t.Fatalf("shared query deadline=%s, present=%t", deadline, ok)
			}
			wantErr := context.DeadlineExceeded
			if closeGame {
				service.Close()
				wantErr = context.Canceled
			} else {
				time.Sleep(attackDataLoadTimeout)
			}
			if got := <-result; !errors.Is(got.err, wantErr) {
				t.Fatalf("shared query lifetime: got %v, want %v", got.err, wantErr)
			}
		})
	}
}

func TestAttackDataDoesNotCacheSharedQueryErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, _, flags := newAttackFlightGame(t)
		first := startAttackFlight(t.Context(), service, nil)
		query := <-flags.entered
		wantErr := errors.New("temporary attack data failure")
		query.err = wantErr
		close(query.release)
		if got := <-first; !errors.Is(got.err, wantErr) {
			t.Fatalf("repository error lost: %v", got.err)
		}
		retry := startAttackFlight(t.Context(), service, nil)
		next := <-flags.entered
		close(next.release)
		if got := <-retry; got.err != nil || flags.calls.Load() != 2 {
			t.Fatalf("retry reused failed query: %+v calls=%d", got, flags.calls.Load())
		}
	})
}

func TestAttackDataSharedGroupsUseEachCallersTaskNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, repo, flags := newAttackFlightGame(t)
		repo.tasks = repo.tasks[:1]
		leader := startAttackFlight(t.Context(), service, nil)
		query := <-flags.entered
		repo.mu.Lock()
		repo.tasks[0].Name = "renamed"
		repo.mu.Unlock()
		filter := "renamed"
		waiter := startAttackFlight(t.Context(), service, &filter)
		synctest.Wait()
		oldName := "second"
		missing, err := service.AttackData(t.Context(), &oldName)
		if err != nil || missing == nil || len(missing) != 0 {
			t.Fatalf("old name should match no current task: data=%v err=%v", missing, err)
		}
		if flags.calls.Load() != 1 {
			t.Fatalf("rename or filter created another query: %d", flags.calls.Load())
		}
		close(query.release)
		oldResult, newResult := <-leader, <-waiter
		if oldResult.err != nil || newResult.err != nil {
			t.Fatalf("shared query failed: old=%v new=%v", oldResult.err, newResult.err)
		}
		if _, ok := oldResult.data[oldName]; !ok || len(oldResult.data) != 1 {
			t.Fatalf("leader metadata changed: %v", oldResult.data)
		}
		if _, ok := newResult.data[filter]; !ok || len(newResult.data) != 1 {
			t.Fatalf("waiter reused leader metadata: %v", newResult.data)
		}
	})
}

func TestAttackDataGroupedSnapshotPreservesAliasesOrderAndEmptyServices(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, repo, flags := newAttackFlightGame(t)
		firstID, secondID := repo.tasks[1].ID, repo.tasks[0].ID
		repo.tasks[0].Name = "alias"
		repo.tasks[1].Name = "alias"
		repo.tasks = append(repo.tasks, domain.Task{
			ID:          uuid.MustParse("00000000-0000-7000-8000-000000000003"),
			Name:        "empty",
			CheckerType: "pfr",
		})
		flags.rows = []domain.AttackDatum{
			{
				TaskID:     secondID,
				TeamIP:     "10.0.0.1",
				PublicData: "second",
			},
			{
				TaskID:     firstID,
				TeamIP:     "10.0.0.1",
				PublicData: "first",
			},
			{
				TaskID:     firstID,
				TeamIP:     "10.0.0.1",
				PublicData: "duplicate",
			},
			{
				TaskID:     secondID,
				TeamIP:     "10.0.0.2",
				PublicData: "other team",
			},
			{
				TaskID:     firstID,
				TeamIP:     "10.0.0.1",
				PublicData: "duplicate",
			},
		}
		leader := startAttackFlight(t.Context(), service, nil)
		query := <-flags.entered
		waiter := joinedAttackFlight(t.Context(), service)
		close(query.release)
		want := map[string]map[string][]string{
			"alias": {
				"10.0.0.1": {"second", "first", "duplicate", "duplicate"},
				"10.0.0.2": {"other team"},
			},
			"empty": {},
		}
		first := <-leader
		if first.err != nil || !reflect.DeepEqual(first.data, want) {
			t.Fatalf("grouped result changed representation: data=%v err=%v", first.data, first.err)
		}
		first.data["alias"]["10.0.0.1"][0] = "mutated"
		first.data["alias"]["10.0.0.2"] = append(first.data["alias"]["10.0.0.2"], "added")
		delete(first.data["alias"], "10.0.0.1")
		first.data["empty"]["new team"] = []string{"owned"}
		second := <-waiter
		if second.err != nil || !reflect.DeepEqual(second.data, want) {
			t.Fatalf("caller mutated another result: data=%v err=%v", second.data, second.err)
		}
		if flags.rows[0].PublicData != "second" || flags.calls.Load() != 1 {
			t.Fatal("shared rows were mutated or query was not shared")
		}
	})
}

func TestGroupAttackDataRejectsCanceledOrExpiredContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cases := [][]domain.AttackDatum{
		nil,
		{
			{
				PublicData: "hint",
			},
		},
	}
	for _, rows := range cases {
		if _, err := groupAttackData(ctx, rows); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled grouping returned %v", err)
		}
	}
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if _, err := groupAttackData(expired, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired grouping returned %v", err)
	}
}

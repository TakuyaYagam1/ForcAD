package usecase

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type bulkGameRepository struct {
	outbound.GameRepository

	teamIDs   []uuid.UUID
	taskIDs   []uuid.UUID
	teamCount int32
	taskCount int32
	teamErr   error
	taskErr   error
}

func (r *bulkGameRepository) DeactivateTeams(_ context.Context, ids []uuid.UUID) (int32, error) {
	r.teamIDs = append([]uuid.UUID(nil), ids...)
	return r.teamCount, r.teamErr
}

func (r *bulkGameRepository) DeactivateTasks(_ context.Context, ids []uuid.UUID) (int32, error) {
	r.taskIDs = append([]uuid.UUID(nil), ids...)
	return r.taskCount, r.taskErr
}

func TestGameBulkDeactivationRejectsInvalidIDs(t *testing.T) {
	valid := bulkTestIDs(101)
	tests := []struct {
		name string
		ids  []uuid.UUID
	}{
		{
			name: "empty",
			ids:  nil,
		},
		{
			name: "nil UUID",
			ids:  []uuid.UUID{uuid.Nil()},
		},
		{
			name: "duplicate",
			ids:  []uuid.UUID{valid[0], valid[0]},
		},
		{
			name: "over limit",
			ids:  valid,
		},
	}
	operations := []struct {
		name  string
		apply func(*Game, context.Context, []uuid.UUID) (int32, error)
	}{
		{
			name:  "teams",
			apply: (*Game).DeactivateTeams,
		},
		{
			name:  "tasks",
			apply: (*Game).DeactivateTasks,
		},
	}

	for _, operation := range operations {
		for _, tt := range tests {
			t.Run(operation.name+"/"+tt.name, func(t *testing.T) {
				repo := &bulkGameRepository{}
				game := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
				t.Cleanup(game.Close)

				count, err := operation.apply(game, t.Context(), tt.ids)
				var validation *domain.ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("error = %v, want ValidationError", err)
				}
				if count != 0 {
					t.Fatalf("count = %d, want 0", count)
				}
				if repo.teamIDs != nil || repo.taskIDs != nil {
					t.Fatalf("repository called for invalid IDs: teams=%v tasks=%v", repo.teamIDs, repo.taskIDs)
				}
				select {
				case <-game.changed:
					t.Fatal("invalid request marked game as changed")
				default:
				}
			})
		}
	}
}

func TestGameBulkDeactivationReturnsChangedCountAndInvalidatesGame(t *testing.T) {
	ids := bulkTestIDs(2)
	operations := []struct {
		name      string
		count     int32
		configure func(*bulkGameRepository)
		apply     func(*Game, context.Context, []uuid.UUID) (int32, error)
		requested func(*bulkGameRepository) []uuid.UUID
	}{
		{
			name:  "teams",
			count: 2,
			configure: func(repo *bulkGameRepository) {
				repo.teamCount = 2
			},
			apply: (*Game).DeactivateTeams,
			requested: func(repo *bulkGameRepository) []uuid.UUID {
				return repo.teamIDs
			},
		},
		{
			name:  "tasks",
			count: 1,
			configure: func(repo *bulkGameRepository) {
				repo.taskCount = 1
			},
			apply: (*Game).DeactivateTasks,
			requested: func(repo *bulkGameRepository) []uuid.UUID {
				return repo.taskIDs
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			repo := &bulkGameRepository{}
			operation.configure(repo)
			game := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
			t.Cleanup(game.Close)

			count, err := operation.apply(game, t.Context(), ids)
			if err != nil {
				t.Fatal(err)
			}
			if count != operation.count {
				t.Fatalf("count = %d, want %d", count, operation.count)
			}
			requested := operation.requested(repo)
			if len(requested) != len(ids) || requested[0] != ids[0] || requested[1] != ids[1] {
				t.Fatalf("repository IDs = %v, want %v", requested, ids)
			}
			select {
			case <-game.changed:
			default:
				t.Fatal("successful deactivation did not mark game as changed")
			}
		})
	}
}

func TestGameBulkDeactivationDoesNotInvalidateAfterRepositoryError(t *testing.T) {
	wantErr := errors.New("write failed")
	ids := bulkTestIDs(1)
	operations := []struct {
		name      string
		configure func(*bulkGameRepository)
		apply     func(*Game, context.Context, []uuid.UUID) (int32, error)
	}{
		{
			name: "teams",
			configure: func(repo *bulkGameRepository) {
				repo.teamErr = wantErr
			},
			apply: (*Game).DeactivateTeams,
		},
		{
			name: "tasks",
			configure: func(repo *bulkGameRepository) {
				repo.taskErr = wantErr
			},
			apply: (*Game).DeactivateTasks,
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			repo := &bulkGameRepository{}
			operation.configure(repo)
			game := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
			t.Cleanup(game.Close)

			count, err := operation.apply(game, t.Context(), ids)
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			if count != 0 {
				t.Fatalf("count = %d, want 0", count)
			}
			select {
			case <-game.changed:
				t.Fatal("failed deactivation marked game as changed")
			default:
			}
		})
	}
}

func bulkTestIDs(count int) []uuid.UUID {
	ids := make([]uuid.UUID, count)
	for i := range ids {
		ids[i][15] = byte(i + 1)
	}
	return ids
}

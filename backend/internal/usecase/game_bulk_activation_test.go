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

type activationRepository struct {
	outbound.GameRepository

	teamIDs []uuid.UUID
	taskIDs []uuid.UUID
	count   int32
	err     error
}

func (r *activationRepository) ActivateTeams(_ context.Context, ids []uuid.UUID) (int32, error) {
	r.teamIDs = append([]uuid.UUID(nil), ids...)
	return r.count, r.err
}

func (r *activationRepository) ActivateTasks(_ context.Context, ids []uuid.UUID) (int32, error) {
	r.taskIDs = append([]uuid.UUID(nil), ids...)
	return r.count, r.err
}

func TestGameBulkActivationValidatesAndNotifiesChanges(t *testing.T) {
	operations := []struct {
		name  string
		apply func(*Game, context.Context, []uuid.UUID) (int32, error)
		ids   func(*activationRepository) []uuid.UUID
	}{
		{
			name:  "teams",
			apply: (*Game).ActivateTeams,
			ids: func(r *activationRepository) []uuid.UUID {
				return r.teamIDs
			},
		},
		{
			name:  "services",
			apply: (*Game).ActivateTasks,
			ids: func(r *activationRepository) []uuid.UUID {
				return r.taskIDs
			},
		},
	}
	ids := bulkTestIDs(2)
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			repo := &activationRepository{
				count: 2,
			}
			game := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
			t.Cleanup(game.Close)
			count, err := operation.apply(game, t.Context(), []uuid.UUID{ids[0], ids[0]})
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || count != 0 || operation.ids(repo) != nil {
				t.Fatalf("duplicate IDs: count=%d err=%v repository IDs=%v", count, err, operation.ids(repo))
			}
			select {
			case <-game.changed:
				t.Fatal("invalid activation triggered a game refresh")
			default:
			}
			count, err = operation.apply(game, t.Context(), ids)
			if err != nil || count != 2 {
				t.Fatalf("activate: count=%d err=%v, want 2 and nil", count, err)
			}
			if got := operation.ids(repo); len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
				t.Fatalf("activated IDs = %v, want %v", got, ids)
			}
			select {
			case <-game.changed:
			default:
				t.Fatal("activation did not refresh the game")
			}
			repo.count = 0
			count, err = operation.apply(game, t.Context(), ids)
			if count != 0 || err != nil {
				t.Fatalf("repeat activation: count=%d err=%v", count, err)
			}
			select {
			case <-game.changed:
				t.Fatal("unchanged activation triggered a game refresh")
			default:
			}
		})
	}
}

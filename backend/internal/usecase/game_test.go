package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type boardRepository struct {
	outbound.GameRepository

	board domain.BoardData
}

type stateErrorRepository struct {
	outbound.GameRepository

	err error
}

func (r *stateErrorRepository) ChangeState(context.Context, string, *int64, time.Time) error {
	return r.err
}

func (r *boardRepository) Board(context.Context) (domain.BoardData, error) { return r.board, nil }

func TestScoreboardRetainsCompletedResultsDuringNextRound(t *testing.T) {
	firstID := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	secondID := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	inactiveID := uuid.MustParse("00000000-0000-7000-8000-000000000003")
	taskID := uuid.MustParse("00000000-0000-7000-8000-000000000004")
	snapshot, err := json.Marshal(domain.GameState{Round: 4, RoundStart: 1700000000, TeamTasks: []domain.PublicCell{
		{TeamID: firstID, TaskID: taskID, Score: 100, Status: 101, Checks: 4, ChecksPassed: 3},
		{TeamID: secondID, TaskID: taskID, Score: 100, Status: 101, Checks: 4, ChecksPassed: 4},
		{TeamID: inactiveID, TaskID: taskID, Score: 9000, Status: 101, Checks: 4, ChecksPassed: 4},
	}})
	if err != nil {
		t.Fatal(err)
	}
	repo := &boardRepository{board: domain.BoardData{
		Lifecycle: domain.Lifecycle{Config: domain.GameConfig{RealRound: 5, GameRunning: true}, Session: domain.Session{Snapshot: snapshot}},
		Teams:     []domain.Team{{ID: firstID, Name: "first", Token: "private-one"}, {ID: secondID, Name: "second", Token: "private-two"}},
		Tasks:     []domain.Task{{ID: taskID, Name: "service", Checker: "private/checker"}},
		Cells:     []domain.Cell{{TeamID: firstID, TaskID: taskID, Score: 1000, Status: 104, PrivateMessage: "private-message"}, {TeamID: secondID, TaskID: taskID, Score: 1000, Status: 104}},
	}}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	board, err := service.Scoreboard(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if board.State.Round != 4 || board.Runtime.Round != 5 || len(board.State.TeamTasks) != 2 || board.State.TeamTasks[0].Score != 100 {
		t.Fatalf("partial round leaked: %+v", board)
	}
	standings, err := service.Standings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(standings) != 2 || standings[0].Team != "second" || standings[0].Score != 100 || standings[1].Score != 75 {
		t.Fatalf("SLA standings: %+v", standings)
	}
	encoded, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	for _, team := range raw["teams"].([]any) {
		if _, ok := team.(map[string]any)["token"]; ok {
			t.Fatal("team token in public scoreboard")
		}
	}
	for _, task := range raw["tasks"].([]any) {
		if _, ok := task.(map[string]any)["checker"]; ok {
			t.Fatal("checker command in public scoreboard")
		}
	}
}

func TestInvalidSnapshotDoesNotFallBackToLiveScores(t *testing.T) {
	repo := &boardRepository{board: domain.BoardData{Lifecycle: domain.Lifecycle{Session: domain.Session{Snapshot: []byte(`{invalid`)}}}}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	if _, err := service.Scoreboard(t.Context()); err == nil {
		t.Fatal("corrupt completed snapshot silently replaced")
	}
}

func TestStandingsBreakScoreTiesByUUID(t *testing.T) {
	firstID := uuid.MustParse("01912345-6789-7000-8000-00000000000a")
	secondID := uuid.MustParse("01912345-6789-7000-8000-00000000000b")
	repo := &boardRepository{board: domain.BoardData{
		Teams: []domain.Team{{ID: secondID, Name: "second"}, {ID: firstID, Name: "first"}},
		Cells: []domain.Cell{
			{TeamID: secondID, Score: 50, Checks: 1, ChecksPassed: 1},
			{TeamID: firstID, Score: 50, Checks: 1, ChecksPassed: 1},
		},
	}}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	standings, err := service.Standings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(standings) != 2 || standings[0].Team != "first" || standings[1].Team != "second" {
		t.Fatalf("equal-score ordering: %+v", standings)
	}
}

func TestMutationErrorsKeepContextAndTypedMessage(t *testing.T) {
	stateErr := &domain.StateError{Message: "Game has already finished"}
	service := NewGame(&stateErrorRepository{err: stateErr}, nil, nil, slog.New(slog.DiscardHandler))
	if err := service.ChangeState(context.Background(), "finish", nil); !errors.Is(err, stateErr) {
		t.Fatalf("ChangeState() error = %v, want wrapped state error", err)
	} else if !strings.HasPrefix(err.Error(), "usecase.Game.ChangeState - ") {
		t.Fatalf("ChangeState() error has no location: %v", err)
	}

	service = NewGame(nil, nil, nil, slog.New(slog.DiscardHandler))
	_, err := service.SaveTask(context.Background(), domain.Task{Puts: 1025}, true)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Message != "Total PUT and GET count must not exceed 1024" {
		t.Fatalf("SaveTask() error lost validation details: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "usecase.Game.SaveTask - ") {
		t.Fatalf("SaveTask() error has no location: %v", err)
	}
}

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

type attackDataRepository struct {
	outbound.GameRepository

	lifecycle domain.Lifecycle
	tasks     []domain.Task
}

func (r *attackDataRepository) Lifecycle(context.Context) (domain.Lifecycle, error) {
	return r.lifecycle, nil
}

func (r *attackDataRepository) Tasks(context.Context, bool) ([]domain.Task, error) {
	return r.tasks, nil
}

type attackDataFlags struct {
	outbound.FlagRepository

	requested []uuid.UUID
	rows      []domain.AttackDatum
}

func (f *attackDataFlags) AttackData(_ context.Context, _ int32, taskIDs []uuid.UUID) ([]domain.AttackDatum, error) {
	f.requested = append([]uuid.UUID(nil), taskIDs...)
	selected := make(map[uuid.UUID]struct{}, len(taskIDs))
	for _, id := range taskIDs {
		selected[id] = struct{}{}
	}
	rows := make([]domain.AttackDatum, 0, len(f.rows))
	for _, row := range f.rows {
		if _, ok := selected[row.TaskID]; ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (r *stateErrorRepository) ChangeState(context.Context, string, *int64, time.Time) error {
	return r.err
}

func (r *boardRepository) Board(context.Context) (domain.BoardData, error) { return r.board, nil }

func TestScoreboardShowsFirstRoundChecks(t *testing.T) {
	teamID := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	taskID := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	snapshot, err := json.Marshal(domain.GameState{Round: 0, TeamTasks: []domain.PublicCell{
		{TeamID: teamID, TaskID: taskID, Status: -1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	repo := &boardRepository{board: domain.BoardData{
		Lifecycle: domain.Lifecycle{
			Config:  domain.GameConfig{RealRound: 1, GameRunning: true},
			Session: domain.Session{Snapshot: snapshot},
		},
		Teams: []domain.Team{{ID: teamID}},
		Tasks: []domain.Task{{ID: taskID}},
		Cells: []domain.Cell{{TeamID: teamID, TaskID: taskID, Status: 102, Checks: 1}},
	}}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	board, err := service.Scoreboard(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if board.Runtime.Round != 1 || len(board.State.TeamTasks) != 1 || board.State.TeamTasks[0].Status != 102 {
		t.Fatalf("first round CHECK hidden by round zero snapshot: %+v", board)
	}
}

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

func TestAttackDataFiltersByExactService(t *testing.T) {
	firstID := uuid.MustParse("01912345-6789-7000-8000-00000000000a")
	secondID := uuid.MustParse("01912345-6789-7000-8000-00000000000b")
	flags := &attackDataFlags{rows: []domain.AttackDatum{
		{TaskID: firstID, TeamIP: "192.0.2.1", PublicData: "first-flag"},
		{TaskID: secondID, TeamIP: "192.0.2.2", PublicData: "second-flag"},
	}}
	repo := &attackDataRepository{
		lifecycle: domain.Lifecycle{Config: domain.GameConfig{RealRound: 7}},
		tasks: []domain.Task{
			{ID: firstID, Name: "service-one", CheckerType: "pfr"},
			{ID: secondID, Name: "service-two", CheckerType: "_pfr_tcp_"},
			{Name: "ignored", CheckerType: "other"},
		},
	}
	service := NewGame(repo, flags, nil, slog.New(slog.DiscardHandler))
	name := "service-one"
	data, err := service.AttackData(t.Context(), &name)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 || len(data[name]) != 1 || data[name]["192.0.2.1"][0] != "first-flag" {
		t.Fatalf("filtered attack data = %#v", data)
	}
	if len(flags.requested) != 1 || flags.requested[0] != firstID {
		t.Fatalf("requested flag task IDs = %v, want only %s", flags.requested, firstID)
	}
}

func TestAttackDataWithoutServicePreservesFullShape(t *testing.T) {
	firstID := uuid.MustParse("01912345-6789-7000-8000-00000000000a")
	secondID := uuid.MustParse("01912345-6789-7000-8000-00000000000b")
	flags := &attackDataFlags{rows: []domain.AttackDatum{{TaskID: firstID, TeamIP: "192.0.2.1", PublicData: "flag"}}}
	repo := &attackDataRepository{
		lifecycle: domain.Lifecycle{Config: domain.GameConfig{RealRound: 7}},
		tasks: []domain.Task{
			{ID: firstID, Name: "service-one", CheckerType: "pfr"},
			{ID: secondID, Name: "service-two", CheckerType: "_pfr_tcp_"},
			{Name: "ignored", CheckerType: "other"},
		},
	}
	service := NewGame(repo, flags, nil, slog.New(slog.DiscardHandler))
	data, err := service.AttackData(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 || len(data["service-one"]) != 1 || data["service-one"]["192.0.2.1"][0] != "flag" || data["service-two"] == nil || len(data["service-two"]) != 0 {
		t.Fatalf("full attack data = %#v", data)
	}
	if len(flags.requested) != 2 || flags.requested[0] != firstID || flags.requested[1] != secondID {
		t.Fatalf("requested flag task IDs = %v, want [%s %s]", flags.requested, firstID, secondID)
	}
	flags.requested = nil
	name := "service-two"
	filtered, err := service.AttackData(t.Context(), &name)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[name] == nil || len(filtered[name]) != 0 {
		t.Fatalf("service without flags = %#v, want an empty team map", filtered)
	}
	if len(flags.requested) != 1 || flags.requested[0] != secondID {
		t.Fatalf("requested flag task IDs = %v, want only %s", flags.requested, secondID)
	}
}

func TestAttackDataMissingServiceReturnsEmptyObject(t *testing.T) {
	repo := &attackDataRepository{
		lifecycle: domain.Lifecycle{Config: domain.GameConfig{RealRound: 7}},
		tasks:     []domain.Task{{Name: "service-one", CheckerType: "pfr"}},
	}
	flags := &attackDataFlags{}
	service := NewGame(repo, flags, nil, slog.New(slog.DiscardHandler))
	for _, name := range []string{"missing", ""} {
		data, err := service.AttackData(t.Context(), &name)
		if err != nil {
			t.Fatal(err)
		}
		if data == nil || len(data) != 0 {
			t.Fatalf("service %q attack data = %#v, want empty object", name, data)
		}
		if len(flags.requested) != 0 {
			t.Fatalf("service %q queried flags for task IDs %v", name, flags.requested)
		}
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

package game

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"sort"
	"strings"
	"time"
	"uuid"

	cachekit "github.com/wahrwelt-kit/go-cachekit"
	"golang.org/x/sync/singleflight"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

const (
	maxCheckerActions    = 1024
	maxNameLength        = 255
	maxCheckerPathLength = 1024
	maxCheckerTypeLength = 32
)

type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

type Service struct {
	repo           outbound.GameRepository
	flags          outbound.FlagRepository
	publisher      Publisher
	logger         *slog.Logger
	snapshot       *cachekit.CachedValue[domain.Scoreboard]
	runtimeFlights singleflight.Group
	runtimeCtx     context.Context
	runtimeCancel  context.CancelFunc
	changed        chan struct{}
}

const (
	runtimeLifecycleKey     = "runtime-lifecycle"
	runtimeLifecycleTimeout = 5 * time.Second
)

func New(repo outbound.GameRepository, flags outbound.FlagRepository, publisher Publisher, logger *slog.Logger) *Service {
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	return &Service{repo: repo, flags: flags, publisher: publisher, logger: logger,
		snapshot:   cachekit.MustNewCachedValue[domain.Scoreboard]("scoreboard", 500*time.Millisecond, cachekit.WithLoadTimeout(5*time.Second)),
		runtimeCtx: runtimeCtx, runtimeCancel: runtimeCancel, changed: make(chan struct{}, 1)}
}

func (s *Service) Close() {
	s.runtimeCancel()
}

func (s *Service) Lifecycle(ctx context.Context) (domain.Lifecycle, error) {
	return s.repo.Lifecycle(ctx)
}
func (s *Service) Runtime(ctx context.Context) (domain.RuntimeStatus, error) {
	const location = "game.Service.Runtime"
	if err := ctx.Err(); err != nil {
		return domain.RuntimeStatus{}, fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	if err := s.runtimeCtx.Err(); err != nil {
		return domain.RuntimeStatus{}, fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	resultCh := s.runtimeFlights.DoChan(runtimeLifecycleKey, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeLifecycleTimeout)
		//nolint:contextcheck // The shared load is request-independent by design; Service.Close and its timeout own cancellation.
		stopClose := context.AfterFunc(s.runtimeCtx, cancel)
		defer stopClose()
		defer cancel()
		if err := s.runtimeCtx.Err(); err != nil {
			return nil, err
		}
		return s.repo.Lifecycle(loadCtx)
	})
	var result singleflight.Result
	select {
	case <-ctx.Done():
		return domain.RuntimeStatus{}, fmt.Errorf("%s - read game lifecycle: %w", location, ctx.Err())
	case result = <-resultCh:
	}
	if err := ctx.Err(); err != nil {
		return domain.RuntimeStatus{}, fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	if result.Err != nil {
		return domain.RuntimeStatus{}, fmt.Errorf("%s - read game lifecycle: %w", location, result.Err)
	}
	life, ok := result.Val.(domain.Lifecycle)
	if !ok {
		return domain.RuntimeStatus{}, fmt.Errorf("%s - invalid shared lifecycle result type %T", location, result.Val)
	}
	return life.Runtime(time.Now()), nil
}
func (s *Service) Teams(ctx context.Context, active bool) ([]domain.Team, error) {
	return s.repo.Teams(ctx, active)
}
func (s *Service) Tasks(ctx context.Context, active bool) ([]domain.Task, error) {
	return s.repo.Tasks(ctx, active)
}
func (s *Service) History(ctx context.Context, team uuid.UUID, task *uuid.UUID) ([]domain.Cell, error) {
	return s.repo.History(ctx, team, task)
}
func (s *Service) PublicHistory(ctx context.Context, team uuid.UUID) ([]domain.PublicCell, error) {
	return s.repo.PublicHistory(ctx, team)
}

func (s *Service) Scoreboard(ctx context.Context) (domain.Scoreboard, error) {
	return s.snapshot.Get(ctx, s.loadScoreboard)
}

func (s *Service) loadScoreboard(ctx context.Context) (domain.Scoreboard, error) {
	const location = "game.Service.loadScoreboard"
	data, err := s.repo.Board(ctx)
	if err != nil {
		return domain.Scoreboard{}, fmt.Errorf("%s - load scoreboard data: %w", location, err)
	}
	life, teams, tasks, cells := data.Lifecycle, data.Teams, data.Tasks, data.Cells
	state := domain.GameState{TeamTasks: make([]domain.PublicCell, 0, len(cells))}
	if len(life.Session.Snapshot) > 0 {
		if err = json.Unmarshal(life.Session.Snapshot, &state); err != nil {
			return domain.Scoreboard{}, fmt.Errorf("%s - decode completed-round snapshot: %w", location, err)
		}
	}
	previous := make(map[[2]uuid.UUID]domain.PublicCell, len(state.TeamTasks))
	for _, cell := range state.TeamTasks {
		previous[[2]uuid.UUID{cell.TeamID, cell.TaskID}] = cell
	}
	state.TeamTasks = make([]domain.PublicCell, 0, len(cells))
	for _, cell := range cells {
		public := cell.Public()
		if old, ok := previous[[2]uuid.UUID{cell.TeamID, cell.TaskID}]; ok {
			public = old
		}
		state.TeamTasks = append(state.TeamTasks, public)
	}
	board := domain.Scoreboard{State: state, Config: life.Config, Runtime: life.Runtime(time.Now()), Teams: make([]domain.PublicTeam, 0, len(teams)), Tasks: make([]domain.PublicTask, 0, len(tasks))}
	for _, team := range teams {
		board.Teams = append(board.Teams, team.Public())
	}
	for _, task := range tasks {
		board.Tasks = append(board.Tasks, task.Public())
	}
	return board, nil
}

type Standing struct {
	Pos   int     `json:"pos"`
	Team  string  `json:"team"`
	Score float64 `json:"score"`
}

func (s *Service) Standings(ctx context.Context) ([]Standing, error) {
	const location = "game.Service.Standings"
	board, err := s.Scoreboard(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - load scoreboard: %w", location, err)
	}
	scores := map[uuid.UUID]float64{}
	for _, cell := range board.State.TeamTasks {
		if cell.Checks > 0 {
			scores[cell.TeamID] += cell.Score * float64(cell.ChecksPassed) / float64(cell.Checks)
		}
	}
	teams := append([]domain.PublicTeam(nil), board.Teams...)
	sort.Slice(teams, func(i, j int) bool {
		if scores[teams[i].ID] == scores[teams[j].ID] {
			return teams[i].ID.Compare(teams[j].ID) < 0
		}
		return scores[teams[i].ID] > scores[teams[j].ID]
	})
	result := make([]Standing, 0, len(teams))
	for i, team := range teams {
		result = append(result, Standing{Pos: i + 1, Team: team.Name, Score: math.RoundToEven(scores[team.ID]*100) / 100})
	}
	return result, nil
}

func (s *Service) AttackData(ctx context.Context) (map[string]map[string][]string, error) {
	const location = "game.Service.AttackData"
	life, err := s.repo.Lifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	tasks, err := s.repo.Tasks(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("%s - list active tasks: %w", location, err)
	}
	ids := make([]uuid.UUID, 0, len(tasks))
	names := make(map[uuid.UUID]string, len(tasks))
	data := map[string]map[string][]string{}
	for _, task := range tasks {
		if hasTag(task.CheckerType, "pfr") {
			ids = append(ids, task.ID)
			names[task.ID] = task.Name
			data[task.Name] = map[string][]string{}
		}
	}
	if len(ids) == 0 {
		return data, nil
	}
	flags, err := s.flags.AttackData(ctx, life.Config.RealRound, ids)
	if err != nil {
		return nil, fmt.Errorf("%s - load public attack data: %w", location, err)
	}
	for _, flag := range flags {
		key := names[flag.TaskID]
		data[key][flag.TeamIP] = append(data[key][flag.TeamIP], flag.PublicData)
	}
	return data, nil
}

func hasTag(tags, tag string) bool {
	for _, item := range strings.Split(tags, "_") {
		if item == tag {
			return true
		}
	}
	return false
}

func (s *Service) Changed() {
	s.runtimeFlights.Forget(runtimeLifecycleKey)
	s.snapshot.Invalidate()
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Periodic reconciliation also covers commits from other API and worker replicas.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var previous []byte
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.changed:
		}
		board, err := s.Scoreboard(ctx)
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Warn("scoreboard refresh unavailable", "error", err)
			}
			continue
		}
		encoded, err := json.Marshal(board)
		if err != nil || bytes.Equal(previous, encoded) {
			continue
		}
		if err = s.publisher.Publish(ctx, "init_scoreboard", map[string]any{"data": board}); err != nil {
			if ctx.Err() == nil {
				s.logger.Warn("scoreboard publication unavailable", "error", err)
			}
			continue
		}
		previous = encoded
	}
}

func (s *Service) ChangeState(ctx context.Context, action string, generation *int64) error {
	const location = "game.Service.ChangeState"
	if err := s.repo.ChangeState(ctx, action, generation, time.Now()); err != nil {
		return fmt.Errorf("%s - change game state: %w", location, err)
	}
	s.Changed()
	return nil
}

func (s *Service) SaveTeam(ctx context.Context, team domain.Team, create bool) (domain.Team, error) {
	const location = "game.Service.SaveTeam"
	team.Name = strings.TrimSpace(team.Name)
	team.IP = strings.TrimSpace(team.IP)
	team.LogoPath = strings.TrimSpace(team.LogoPath)
	if create {
		var token [8]byte
		if _, err := rand.Read(token[:]); err != nil {
			return domain.Team{}, fmt.Errorf("%s - generate team token: %w", location, err)
		}
		team.Token = hex.EncodeToString(token[:])
	}
	if team.Name == "" || len([]rune(team.Name)) > maxNameLength {
		return domain.Team{}, fmt.Errorf("%s - invalid team name: %w", location, &domain.ValidationError{Message: "Invalid team name"})
	}
	if _, err := netip.ParseAddr(team.IP); err != nil {
		return domain.Team{}, fmt.Errorf("%s - invalid team address: %w", location, &domain.ValidationError{Message: "Invalid IPv4 or IPv6 address"})
	}
	if !config.ValidLogoPath(team.LogoPath) {
		return domain.Team{}, fmt.Errorf("%s - invalid team logo URL: %w", location, &domain.ValidationError{Message: "Invalid team logo URL"})
	}
	decoded, err := hex.DecodeString(team.Token)
	if err != nil || len(decoded) != 8 || strings.ToLower(team.Token) != team.Token {
		return domain.Team{}, fmt.Errorf("%s - invalid team token format: %w", location, &domain.ValidationError{Message: "Team token must contain exactly 16 lowercase hexadecimal characters"})
	}
	if create {
		team, err = s.repo.CreateTeam(ctx, team)
	} else {
		team, err = s.repo.UpdateTeam(ctx, team)
	}
	if err != nil {
		return domain.Team{}, fmt.Errorf("%s - save team: %w", location, err)
	}
	s.Changed()
	return team, nil
}

func (s *Service) SaveTask(ctx context.Context, task domain.Task, create bool) (domain.Task, error) {
	const location = "game.Service.SaveTask"
	task.Name = strings.TrimSpace(task.Name)
	task.Checker = strings.TrimSpace(task.Checker)
	task.CheckerType = strings.TrimSpace(task.CheckerType)
	if err := validateTask(task); err != nil {
		return domain.Task{}, fmt.Errorf("%s - validate task: %w", location, err)
	}
	var err error
	if create {
		task, err = s.repo.CreateTask(ctx, task)
	} else {
		task, err = s.repo.UpdateTask(ctx, task)
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("%s - save task: %w", location, err)
	}
	s.Changed()
	return task, nil
}

func validateTask(task domain.Task) error {
	const location = "game.validateTask"
	if int64(task.Puts)+int64(task.Gets) > maxCheckerActions {
		return fmt.Errorf("%s - invalid checker action count: %w", location, &domain.ValidationError{
			Message: fmt.Sprintf("Total PUT and GET count must not exceed %d", maxCheckerActions),
		})
	}
	if invalidTaskSettings(task) {
		return fmt.Errorf("%s - invalid task settings: %w", location, &domain.ValidationError{Message: "Invalid task settings"})
	}
	return nil
}

func invalidTaskSettings(task domain.Task) bool {
	return task.Name == "" || len([]rune(task.Name)) > maxNameLength || task.Checker == "" ||
		len(task.Checker) > maxCheckerPathLength || len(task.EnvPath) > maxCheckerPathLength ||
		len(task.CheckerType) > maxCheckerTypeLength ||
		task.Puts < 0 || task.Gets < 0 || task.Places < 1 || task.CheckerTimeout < 1 ||
		task.GetPeriod < 1 || task.DefaultScore < 0
}

func (s *Service) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	const location = "game.Service.DeleteTeam"
	err := s.repo.DeactivateTeam(ctx, id)
	if err != nil {
		return fmt.Errorf("%s - deactivate team: %w", location, err)
	}
	s.Changed()
	return nil
}
func (s *Service) DeleteTask(ctx context.Context, id uuid.UUID) error {
	const location = "game.Service.DeleteTask"
	err := s.repo.DeactivateTask(ctx, id)
	if err != nil {
		return fmt.Errorf("%s - deactivate task: %w", location, err)
	}
	s.Changed()
	return nil
}

package domain

import (
	"time"
	"uuid"
)

type GameConfig struct {
	ID               int32     `json:"id"`
	FlagLifetime     int32     `json:"flag_lifetime"`
	GameHardness     float64   `json:"game_hardness"`
	Inflation        bool      `json:"inflation"`
	VolgaAttacksMode bool      `json:"volga_attacks_mode"`
	RoundTime        int32     `json:"round_time"`
	Rounds           *int32    `json:"rounds"`
	Mode             string    `json:"mode"`
	Timezone         string    `json:"timezone"`
	StartTime        time.Time `json:"start_time"`
	RealRound        int32     `json:"real_round"`
	GameRunning      bool      `json:"game_running"`
}

type Team struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	IP          string    `json:"ip"`
	Token       string    `json:"token"`
	Highlighted bool      `json:"highlighted"`
	Active      bool      `json:"active"`
	LogoPath    string    `json:"logo_path"`
}

type PublicTeam struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	IP          string    `json:"ip"`
	Highlighted bool      `json:"highlighted"`
	Active      bool      `json:"active"`
	LogoPath    string    `json:"logo_path"`
}

func (t Team) Public() PublicTeam {
	return PublicTeam{ID: t.ID, Name: t.Name, IP: t.IP, Highlighted: t.Highlighted, Active: t.Active, LogoPath: t.LogoPath}
}

type Task struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Checker        string    `json:"checker"`
	EnvPath        string    `json:"env_path"`
	Gets           int32     `json:"gets"`
	Puts           int32     `json:"puts"`
	Places         int32     `json:"places"`
	CheckerTimeout int32     `json:"checker_timeout"`
	CheckerType    string    `json:"checker_type"`
	GetPeriod      int32     `json:"get_period"`
	DefaultScore   int32     `json:"default_score"`
	Active         bool      `json:"active"`
}

type PublicTask struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

func (t Task) Public() PublicTask { return PublicTask{ID: t.ID, Name: t.Name} }

type Cell struct {
	ID             uuid.UUID `json:"id"`
	CreatedAt      time.Time `json:"-"`
	TeamID         uuid.UUID `json:"team_id"`
	TaskID         uuid.UUID `json:"task_id"`
	Status         int32     `json:"status"`
	Stolen         int32     `json:"stolen"`
	Lost           int32     `json:"lost"`
	Score          float64   `json:"score"`
	Checks         int32     `json:"checks"`
	ChecksPassed   int32     `json:"checks_passed"`
	PublicMessage  string    `json:"public_message"`
	PrivateMessage string    `json:"private_message"`
	Command        string    `json:"command"`
	Round          int32     `json:"round"`
	Timestamp      string    `json:"timestamp,omitempty"`
}

type PublicCell struct {
	TeamID       uuid.UUID `json:"team_id"`
	TaskID       uuid.UUID `json:"task_id"`
	Status       int32     `json:"status"`
	Stolen       int32     `json:"stolen"`
	Lost         int32     `json:"lost"`
	Score        float64   `json:"score"`
	Checks       int32     `json:"checks"`
	ChecksPassed int32     `json:"checks_passed"`
	Message      string    `json:"message"`
	Round        int32     `json:"round"`
	Timestamp    string    `json:"timestamp,omitempty"`
}

func (c Cell) Public() PublicCell {
	return PublicCell{TeamID: c.TeamID, TaskID: c.TaskID, Status: c.Status, Stolen: c.Stolen, Lost: c.Lost, Score: c.Score, Checks: c.Checks, ChecksPassed: c.ChecksPassed, Message: c.PublicMessage, Round: c.Round, Timestamp: c.Timestamp}
}

type GameState struct {
	Round      int32        `json:"round"`
	RoundStart int64        `json:"round_start"`
	TeamTasks  []PublicCell `json:"team_tasks"`
}

type RuntimeStatus struct {
	Phase            string    `json:"phase"`
	Practice         bool      `json:"practice"`
	ResetPending     bool      `json:"reset_pending"`
	Generation       int64     `json:"generation"`
	ScheduledStart   time.Time `json:"scheduled_start"`
	CanStart         bool      `json:"can_start"`
	CanStartPractice bool      `json:"can_start_practice"`
	Round            int32     `json:"round"`
	RoundStart       *int64    `json:"round_start"`
	PausedAt         *float64  `json:"paused_at"`
	PausedSeconds    float64   `json:"paused_seconds"`
	RoundTime        int32     `json:"round_time"`
	RoundWaiting     bool      `json:"round_waiting"`
	ResultsPending   bool      `json:"results_pending"`
}

type Scoreboard struct {
	State   GameState     `json:"state"`
	Teams   []PublicTeam  `json:"teams"`
	Tasks   []PublicTask  `json:"tasks"`
	Config  GameConfig    `json:"config"`
	Runtime RuntimeStatus `json:"runtime"`
}

type Lifecycle struct {
	Config  GameConfig
	Session Session
	Pending bool
}

// BoardData is read from a single database snapshot so a rehearsal reset cannot
// combine cells from one generation with runtime metadata from another.
type BoardData struct {
	Lifecycle Lifecycle
	Teams     []Team
	Tasks     []Task
	Cells     []Cell
}

func (l Lifecycle) Finished() bool {
	return !l.Config.GameRunning && (l.Config.RealRound > 0 || l.Session.PracticeStart != nil || l.Session.FinalStart != nil)
}

func (l Lifecycle) Runtime(now time.Time) RuntimeStatus {
	r := RuntimeStatus{Phase: "waiting", Practice: l.Session.PracticeStart != nil, ResetPending: l.Session.ResetPending,
		Generation: l.Session.Generation, ScheduledStart: l.Config.StartTime, Round: l.Config.RealRound,
		PausedSeconds: l.Session.PausedSeconds, RoundTime: l.Config.RoundTime, ResultsPending: l.Finished() && l.Pending}
	r.CanStart = l.canStart()
	r.CanStartPractice = r.CanStart && now.Before(l.Config.StartTime)
	r.Phase = l.runtimePhase()
	l.setRoundRuntime(&r, now)
	if l.Session.PausedAt != nil {
		r.PausedAt = new(float64(l.Session.PausedAt.UnixNano()) / 1e9)
	}
	return r
}

func (l Lifecycle) canStart() bool {
	return !l.Config.GameRunning && l.Config.RealRound == 0 && l.Session.PracticeStart == nil &&
		l.Session.FinalStart == nil && !l.Session.ResetPending
}

func (l Lifecycle) runtimePhase() string {
	switch {
	case l.Finished():
		return "finished"
	case l.Session.PausedAt != nil:
		return "paused"
	case l.Config.GameRunning:
		return "running"
	default:
		return "waiting"
	}
}

func (l Lifecycle) setRoundRuntime(status *RuntimeStatus, now time.Time) {
	if l.Session.RoundStartedAt == nil || status.Round <= 0 {
		return
	}
	status.RoundStart = new(l.Session.RoundStartedAt.Unix())
	elapsed := now.Sub(*l.Session.RoundStartedAt).Seconds() - status.PausedSeconds
	if l.Session.PausedAt != nil {
		elapsed -= max(0, now.Sub(*l.Session.PausedAt).Seconds())
	}
	status.RoundWaiting = l.Config.GameRunning && l.Pending && elapsed >= float64(status.RoundTime)
}

type JobPayload struct {
	Team       Team   `json:"team"`
	Task       Task   `json:"task"`
	Workflow   string `json:"workflow"`
	Generation int64  `json:"generation"`
}

package v1

import (
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func publicTeam(t domain.PublicTeam) api.ParticipantTeam {
	return api.ParticipantTeam{ID: api.UUID(t.ID), Name: t.Name, IP: t.IP, Highlighted: t.Highlighted, Active: t.Active, LogoPath: t.LogoPath}
}

func adminTeam(t domain.Team) *api.AdminTeam {
	return &api.AdminTeam{ID: api.UUID(t.ID), Name: t.Name, IP: t.IP, Token: t.Token, Highlighted: t.Highlighted, Active: t.Active, LogoPath: t.LogoPath}
}

func adminTask(t domain.Task) *api.AdminTask {
	return &api.AdminTask{ID: api.UUID(t.ID), Name: t.Name, Checker: t.Checker, Gets: t.Gets, Puts: t.Puts, Places: t.Places, CheckerTimeout: t.CheckerTimeout, CheckerType: t.CheckerType, EnvPath: t.EnvPath, DefaultScore: float64(t.DefaultScore), GetPeriod: t.GetPeriod, Active: t.Active}
}

func taskInput(t *api.AdminTaskWrite, id uuid.UUID) domain.Task {
	defaultScore := int32(t.DefaultScore) //nolint:gosec // validateTaskInput checks the int32 range before either caller.
	return domain.Task{ID: id, Name: t.Name, Checker: t.Checker, Gets: t.Gets, Puts: t.Puts, Places: t.Places, CheckerTimeout: t.CheckerTimeout, CheckerType: t.CheckerType, EnvPath: t.EnvPath.Or(""), DefaultScore: defaultScore, GetPeriod: t.GetPeriod, Active: t.Active.Or(true)}
}

func gameConfig(c domain.GameConfig) api.GameConfig {
	rounds := api.NilInt32{Null: true}
	if c.Rounds != nil {
		rounds.SetTo(*c.Rounds)
	}
	return api.GameConfig{ID: c.ID, FlagLifetime: c.FlagLifetime, GameHardness: c.GameHardness, Inflation: c.Inflation, VolgaAttacksMode: c.VolgaAttacksMode, RoundTime: c.RoundTime, Rounds: rounds, Mode: c.Mode, Timezone: c.Timezone, StartTime: c.StartTime.Format(time.RFC3339Nano), RealRound: api.NewNilInt32(c.RealRound), GameRunning: api.NewNilBool(c.GameRunning)}
}

func runtimeStatus(r domain.RuntimeStatus) *api.GameRuntimeStatus {
	start, paused := api.NilFloat64{Null: true}, api.NilFloat64{Null: true}
	if r.RoundStart != nil {
		start.SetTo(float64(*r.RoundStart))
	}
	if r.PausedAt != nil {
		paused.SetTo(*r.PausedAt)
	}
	return &api.GameRuntimeStatus{Phase: api.GameRuntimeStatusPhase(r.Phase), Practice: r.Practice, ResetPending: r.ResetPending, Generation: r.Generation, ScheduledStart: r.ScheduledStart.Format(time.RFC3339Nano), CanStart: r.CanStart, CanStartPractice: r.CanStartPractice, Round: r.Round, RoundStart: start, PausedAt: paused, PausedSeconds: r.PausedSeconds, RoundTime: r.RoundTime, RoundWaiting: r.RoundWaiting, ResultsPending: r.ResultsPending}
}

func scoreboard(b domain.Scoreboard) *api.Scoreboard {
	result := &api.Scoreboard{Config: gameConfig(b.Config), Runtime: *runtimeStatus(b.Runtime), Teams: make([]api.ParticipantTeam, 0, len(b.Teams)), Tasks: make([]api.ParticipantTask, 0, len(b.Tasks)), State: api.GameState{Round: b.State.Round, RoundStart: b.State.RoundStart, TeamTasks: make([]api.ScoreboardTeamTask, 0, len(b.State.TeamTasks))}}
	for _, t := range b.Teams {
		result.Teams = append(result.Teams, publicTeam(t))
	}
	for _, t := range b.Tasks {
		result.Tasks = append(result.Tasks, api.ParticipantTask{ID: api.UUID(t.ID), Name: t.Name})
	}
	for _, c := range b.State.TeamTasks {
		result.State.TeamTasks = append(result.State.TeamTasks, api.ScoreboardTeamTask{TeamID: api.UUID(c.TeamID), TaskID: api.UUID(c.TaskID), Status: c.Status, Stolen: c.Stolen, Lost: c.Lost, Score: c.Score, Checks: c.Checks, ChecksPassed: c.ChecksPassed, Message: api.NewNilString(c.Message)})
	}
	return result
}

func publicHistory(c domain.PublicCell) api.ParticipantTeamTaskHistory {
	return api.ParticipantTeamTaskHistory{TeamID: api.UUID(c.TeamID), TaskID: api.UUID(c.TaskID), Round: c.Round, Status: c.Status, Stolen: c.Stolen, Lost: c.Lost, Score: c.Score, Checks: c.Checks, ChecksPassed: c.ChecksPassed, Timestamp: c.Timestamp, Message: api.NewNilString(c.Message)}
}

func adminHistory(c domain.Cell) api.AdminTeamTaskLogEntry {
	return api.AdminTeamTaskLogEntry{ID: api.UUID(c.ID), TeamID: api.UUID(c.TeamID), TaskID: api.UUID(c.TaskID), Round: c.Round, Status: c.Status, Stolen: c.Stolen, Lost: c.Lost, Score: c.Score, Checks: c.Checks, ChecksPassed: c.ChecksPassed, Ts: float64(c.CreatedAt.UnixMicro()) / 1e6, PublicMessage: api.NewNilString(c.PublicMessage), PrivateMessage: api.NewNilString(c.PrivateMessage), Command: api.NewNilString(c.Command)}
}

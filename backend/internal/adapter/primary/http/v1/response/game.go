package response

import (
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func GameConfig(c domain.GameConfig) api.GameConfig {
	rounds := api.NilInt32{
		Null: true,
	}
	if c.Rounds != nil {
		rounds.SetTo(*c.Rounds)
	}
	return api.GameConfig{
		ID:               c.ID,
		FlagLifetime:     c.FlagLifetime,
		GameHardness:     c.GameHardness,
		Inflation:        c.Inflation,
		VolgaAttacksMode: c.VolgaAttacksMode,
		RoundTime:        c.RoundTime,
		Rounds:           rounds,
		Mode:             c.Mode,
		Timezone:         c.Timezone,
		StartTime:        c.StartTime.Format(time.RFC3339Nano),
		RealRound:        api.NewNilInt32(c.RealRound),
		GameRunning:      api.NewNilBool(c.GameRunning),
	}
}

func RuntimeStatus(r domain.RuntimeStatus) *api.GameRuntimeStatus {
	start, paused := api.NilFloat64{
		Null: true,
	}, api.NilFloat64{
		Null: true,
	}
	if r.RoundStart != nil {
		start.SetTo(float64(*r.RoundStart))
	}
	if r.PausedAt != nil {
		paused.SetTo(*r.PausedAt)
	}
	return &api.GameRuntimeStatus{
		Phase:            api.GameRuntimeStatusPhase(r.Phase),
		Practice:         r.Practice,
		ResetPending:     r.ResetPending,
		Generation:       r.Generation,
		ScheduledStart:   r.ScheduledStart.Format(time.RFC3339Nano),
		CanStart:         r.CanStart,
		CanStartPractice: r.CanStartPractice,
		Round:            r.Round,
		RoundStart:       start,
		PausedAt:         paused,
		PausedSeconds:    r.PausedSeconds,
		RoundTime:        r.RoundTime,
		RoundWaiting:     r.RoundWaiting,
		ResultsPending:   r.ResultsPending,
	}
}

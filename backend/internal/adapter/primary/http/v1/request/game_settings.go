package request

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func GameSettings(req *api.AdminGameSettingsUpdate) domain.GameSettingsUpdate {
	var rounds *int32
	if limit, ok := req.Rounds.Get(); ok {
		rounds = &limit
	}
	return domain.GameSettingsUpdate{
		Generation:       req.Generation,
		Rounds:           rounds,
		FlagLifetime:     req.FlagLifetime,
		GameHardness:     req.GameHardness,
		Inflation:        req.Inflation,
		VolgaAttacksMode: req.VolgaAttacksMode,
		RoundTime:        req.RoundTime,
		ExtraChecks:      req.ExtraChecks,
		Mode:             string(req.Mode),
		Timezone:         req.Timezone,
		StartTime:        req.StartTime,
	}
}

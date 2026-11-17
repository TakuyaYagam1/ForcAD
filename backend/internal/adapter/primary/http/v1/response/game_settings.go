package response

import (
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func AdminGameSettings(life domain.Lifecycle) *api.AdminGameSettings {
	return &api.AdminGameSettings{
		Config:      GameConfig(life.Config),
		Runtime:     *RuntimeStatus(life.Runtime(time.Now())),
		ExtraChecks: life.Config.ExtraChecks,
	}
}

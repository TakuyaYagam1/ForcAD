package domain

import "time"

type GameSettingsUpdate struct {
	Generation       int64
	Rounds           *int32
	FlagLifetime     int32
	GameHardness     float64
	Inflation        bool
	VolgaAttacksMode bool
	RoundTime        int32
	ExtraChecks      int32
	Mode             string
	Timezone         string
	StartTime        time.Time
}

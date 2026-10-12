package load_test

import (
	"fmt"
	"os"
	"time"
)

const (
	teamCount          = 20
	batchSize          = 100
	requestTimeout     = 3 * time.Second
	latencyBudget      = time.Second
	generatorLagBudget = 100 * time.Millisecond
)

type stage struct {
	Name     string
	Duration time.Duration
	ReadRPS  int
	FlagRPS  int
	Path     string
	Overload bool
}

var readPaths = []string{
	"/api/client/status/", "/api/client/scoreboard/", "/api/client/teams/",
	"/api/client/tasks/", "/api/client/config/", "/api/client/attack_data/",
}

func loadProfile() (string, []stage, error) {
	name := os.Getenv("LOAD_PROFILE")
	if name == "" {
		name = "smoke"
	}
	warmup, err := durationSetting("LOAD_WARMUP", 10*time.Second)
	if err != nil {
		return name, nil, err
	}
	recovery, err := durationSetting("LOAD_RECOVERY", 30*time.Second)
	if err != nil {
		return name, nil, err
	}
	stages := []stage{{Name: "warmup", Duration: warmup, ReadRPS: 50, FlagRPS: 20}}
	appendStage := func(label string, duration time.Duration, rps int, overload bool, path string) {
		stages = append(stages, stage{Name: label, Duration: duration, ReadRPS: rps, FlagRPS: 40, Overload: overload, Path: path})
	}
	switch name {
	case "smoke", "load", "soak":
		duration := map[string]time.Duration{"smoke": 30 * time.Second, "load": 15 * time.Minute, "soak": 4 * time.Hour}[name]
		duration, err = durationSetting("LOAD_DURATION", duration)
		for part := 1; duration > 0 && err == nil; part++ {
			chunk := min(duration, 5*time.Minute)
			appendStage(fmt.Sprintf("steady-%02d", part), chunk, 500, false, "")
			duration -= chunk
		}
	case "stress", "endpoints":
		var duration time.Duration
		duration, err = durationSetting("LOAD_DURATION", time.Minute)
		paths := []string{""}
		if name == "endpoints" {
			paths = readPaths
		}
		for _, path := range paths {
			for _, rps := range []int{500, 1000, 2500, 5000, 10000} {
				appendStage(fmt.Sprintf("step-%d", rps), duration, rps, true, path)
			}
		}
	case "spike":
		var duration time.Duration
		duration, err = durationSetting("LOAD_DURATION", 30*time.Second)
		appendStage("baseline", duration, 500, false, "")
		appendStage("spike", duration, 10000, true, "")
	default:
		return name, nil, fmt.Errorf("load.loadProfile - unknown profile %q", name)
	}
	if err != nil {
		return name, nil, err
	}
	stages = append(stages, stage{Name: "recovery", Duration: recovery, ReadRPS: 100, FlagRPS: 20})
	var total time.Duration
	for _, s := range stages {
		total += s.Duration
	}
	if total > 23*time.Hour {
		return name, nil, fmt.Errorf("load.loadProfile - total scheduled duration exceeds 23h")
	}
	return name, stages, nil
}

func durationSetting(key string, fallback time.Duration) (time.Duration, error) {
	if raw := os.Getenv(key); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration < time.Second || duration > 12*time.Hour {
			return 0, fmt.Errorf("load.durationSetting - %s must be between 1s and 12h", key)
		}
		return duration, nil
	}
	return fallback, nil
}

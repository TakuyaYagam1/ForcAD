package load_test

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultTeamCount   = 20
	batchSize          = 100
	requestTimeout     = 3 * time.Second
	latencyBudget      = time.Second
	generatorLagBudget = 100 * time.Millisecond
)

var teamCount = defaultTeamCount

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
	readRPS, err := restRPSSetting(name)
	if err != nil {
		return name, nil, err
	}
	warmup, err := durationSetting("LOAD_WARMUP", 10*time.Second)
	if err != nil {
		return name, nil, err
	}
	recovery, err := durationSetting("LOAD_RECOVERY", 30*time.Second)
	if err != nil {
		return name, nil, err
	}
	warmupFlagRPS, steadyFlagRPS, recoveryFlagRPS := 20, 40, 20
	warmupReadRPS, recoveryReadRPS := 50, 100
	if name == "checker" {
		warmupFlagRPS, steadyFlagRPS, recoveryFlagRPS = 2, 2, 2
		warmupReadRPS, recoveryReadRPS = readRPS, readRPS
	}
	stages := []stage{{Name: "warmup", Duration: warmup, ReadRPS: warmupReadRPS, FlagRPS: warmupFlagRPS}}
	appendStage := func(label string, duration time.Duration, rps int, overload bool, path string) {
		stages = append(stages, stage{Name: label, Duration: duration, ReadRPS: rps, FlagRPS: steadyFlagRPS, Overload: overload, Path: path})
	}
	switch name {
	case "checker", "smoke", "load", "soak":
		checkpoint, checkpointErr := durationSetting("LOAD_CHECKPOINT_INTERVAL", 5*time.Minute)
		if checkpointErr != nil {
			return name, nil, checkpointErr
		}
		duration := map[string]time.Duration{"checker": 90 * time.Second, "smoke": 30 * time.Second, "load": 15 * time.Minute, "soak": 4 * time.Hour}[name]
		duration, err = durationSetting("LOAD_DURATION", duration)
		for part := 1; duration > 0 && err == nil; part++ {
			chunk := min(duration, checkpoint)
			appendStage(fmt.Sprintf("steady-%02d", part), chunk, readRPS, false, "")
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
	stages = append(stages, stage{Name: "recovery", Duration: recovery, ReadRPS: recoveryReadRPS, FlagRPS: recoveryFlagRPS})
	var total time.Duration
	for _, s := range stages {
		total += s.Duration
	}
	if total > 23*time.Hour {
		return name, nil, fmt.Errorf("load.loadProfile - total scheduled duration exceeds 23h")
	}
	return name, stages, nil
}

func restRPSSetting(profile string) (int, error) {
	raw := os.Getenv("LOAD_REST_RPS")
	if raw == "" {
		return 500, nil
	}
	switch profile {
	case "checker", "smoke", "load", "soak":
	default:
		return 0, fmt.Errorf("load.restRPSSetting - LOAD_REST_RPS is only supported by checker, smoke, load and soak profiles")
	}
	rps, err := strconv.Atoi(raw)
	if err != nil || rps < 1 || rps > 100000 {
		return 0, fmt.Errorf("load.restRPSSetting - LOAD_REST_RPS must be an integer between 1 and 100000")
	}
	return rps, nil
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

package load_test

import (
	"fmt"
	"os"
	"strconv"
)

// diagnosticFlagWorkerLimit applies an opt-in generator override to flag-burst only.
func diagnosticFlagWorkerLimit(stageName string) (uint64, error) {
	if stageName != "flag-burst" {
		return flagWorkers, nil
	}
	raw := os.Getenv("LOAD_DIAGNOSTIC_BURST_WORKERS")
	if raw == "" {
		return flagWorkers, nil
	}
	workers, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || workers < 1 || workers > readWorkers {
		return 0, fmt.Errorf("load.diagnosticFlagWorkerLimit - LOAD_DIAGNOSTIC_BURST_WORKERS must be an integer between 1 and 1024")
	}
	return workers, nil
}

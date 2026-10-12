package load_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type stageReport struct {
	Name            string       `json:"name"`
	Started         time.Time    `json:"started"`
	Finished        time.Time    `json:"finished"`
	Path            string       `json:"path,omitempty"`
	Seconds         float64      `json:"seconds"`
	Reads           attackReport `json:"reads"`
	Flags           attackReport `json:"flags"`
	Accepted        int64        `json:"accepted"`
	Committed       int64        `json:"committed"`
	IntegrityPassed bool         `json:"integrity_passed"`
}

type suiteReport struct {
	Started           time.Time       `json:"started"`
	Finished          time.Time       `json:"finished"`
	Profile           string          `json:"profile"`
	Diagnostics       bool            `json:"diagnostics"`
	DatabaseStorage   string          `json:"database_storage"`
	ContainerLogging  string          `json:"container_logging"`
	GoVersion         string          `json:"go_version"`
	GeneratorCPUs     int             `json:"generator_cpus"`
	GOMAXPROCS        int             `json:"gomaxprocs"`
	DockerHost        json.RawMessage `json:"docker_host,omitempty"`
	Stages            []stageReport   `json:"stages"`
	WebSockets        streamReport    `json:"websockets"`
	CheckerResults    int64           `json:"checker_results"`
	CorrectnessPassed bool            `json:"correctness_passed"`
	Error             string          `json:"error,omitempty"`
}

func newReport(profile string) (*suiteReport, string, error) {
	directory := os.Getenv("LOAD_REPORT_DIR")
	if directory == "" {
		directory = "reports"
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, "", fmt.Errorf("load.newReport - create directory: %w", err)
	}
	directory, err := os.MkdirTemp(directory, time.Now().UTC().Format("20060102-150405-")+profile+"-")
	if err != nil {
		return nil, "", fmt.Errorf("load.newReport - create run directory: %w", err)
	}
	report := &suiteReport{Started: time.Now().UTC(), Profile: profile, Diagnostics: os.Getenv("LOAD_DIAGNOSTICS") == "1", GoVersion: runtime.Version(), GeneratorCPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0)}
	report.DatabaseStorage = os.Getenv("LOAD_DATABASE_STORAGE")
	if report.DatabaseStorage == "" {
		report.DatabaseStorage = "disk"
	}
	report.ContainerLogging = os.Getenv("LOAD_CONTAINER_LOGGING")
	if report.ContainerLogging == "" {
		report.ContainerLogging = "default"
	}
	return report, directory, nil
}

func (r *suiteReport) save(directory string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("load.suiteReport.save - encode report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "report.json"), append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("load.suiteReport.save - write report: %w", err)
	}
	return nil
}

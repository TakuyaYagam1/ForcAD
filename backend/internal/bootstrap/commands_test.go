package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCapacityUsesStandardDefaults(t *testing.T) {
	for _, key := range []string{
		"DATABASE_MAX_CONNS",
		"FORCAD_API_DATABASE_MAX_CONNS",
		"FORCAD_RECEIVER_DATABASE_MAX_CONNS",
		"FORCAD_WORKER_DATABASE_MAX_CONNS",
		"FORCAD_TICKER_DATABASE_MAX_CONNS",
		"HTTP_MAX_INFLIGHT",
		"FORCAD_API_HTTP_MAX_INFLIGHT",
		"FORCAD_RECEIVER_HTTP_MAX_INFLIGHT",
		"WORKER_CONCURRENCY",
		"FORCAD_CHECKER_MEMORY_LIMIT_MB",
	} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := RuntimeCapacity(t.Context(), nil, &output); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Total            int            `json:"database_connection_ceiling"`
		Roles            map[string]int `json:"database_connections_by_role"`
		CheckerProcesses int            `json:"checker_process_ceiling"`
		CheckerMemoryMiB int            `json:"checker_memory_ceiling_mib"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Total != 120 ||
		report.Roles["api"] != 64 ||
		report.Roles["receiver"] != 32 ||
		report.Roles["worker"] != 16 ||
		report.Roles["ticker"] != 8 ||
		report.CheckerProcesses != 32 ||
		report.CheckerMemoryMiB != 32768 {
		t.Fatalf("capacity=%+v", report)
	}
}

func TestCapacityUsesEveryRolePool(t *testing.T) {
	for key, value := range map[string]string{
		"FORCAD_API_DATABASE_MAX_CONNS":      "16",
		"FORCAD_RECEIVER_DATABASE_MAX_CONNS": "32",
		"FORCAD_WORKER_DATABASE_MAX_CONNS":   "16",
		"FORCAD_TICKER_DATABASE_MAX_CONNS":   "8",
		"WORKER_CONCURRENCY":                 "8",
		"FORCAD_API_HTTP_MAX_INFLIGHT":       "256",
		"FORCAD_RECEIVER_HTTP_MAX_INFLIGHT":  "128",
	} {
		t.Setenv(key, value)
	}
	var output bytes.Buffer
	if err := RuntimeCapacity(t.Context(), nil, &output); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Total            int            `json:"database_connection_ceiling"`
		Roles            map[string]int `json:"database_connections_by_role"`
		CheckerProcesses int            `json:"checker_process_ceiling"`
		HTTP             int            `json:"http_inflight_ceiling"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Total != 200 || report.Roles["api"] != 64 || report.Roles["receiver"] != 64 || report.Roles["worker"] != 64 || report.Roles["ticker"] != 8 || report.HTTP != 1280 || report.CheckerProcesses != 32 {
		t.Fatalf("capacity=%+v", report)
	}
}

func TestDatabaseCommandsBypassPooler(t *testing.T) {
	t.Setenv("DATABASE_POOLER_ADDR", "pooler.example:6432")
	t.Setenv("POSTGRES_HOST", "database.example")
	t.Setenv("DATABASE_URL", "")
	cfg, err := commandConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabasePoolerAddr != "" || !strings.Contains(cfg.DatabaseURL, "@database.example:") {
		t.Fatal("operator database command uses transaction pooling")
	}
}

func TestDatabaseCommandsDoNotInheritAPIPoolMinimum(t *testing.T) {
	t.Setenv("FORCAD_ROLE", "api")
	t.Setenv("FORCAD_API_DATABASE_MAX_CONNS", "32")
	t.Setenv("FORCAD_TICKER_DATABASE_MAX_CONNS", "8")
	t.Setenv("DATABASE_MIN_CONNS", "32")
	cfg, err := commandConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseMinConns != 0 || cfg.DatabaseMaxConns != 8 {
		t.Fatalf("operator command inherited the API pool minimum: min=%d max=%d", cfg.DatabaseMinConns, cfg.DatabaseMaxConns)
	}
}

func TestRunDatabaseCommandWrapsFlagHelp(t *testing.T) {
	err := RunDatabaseCommand(context.Background(), "status", []string{"-h"}, io.Discard)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help error = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(err.Error(), "bootstrap.RunDatabaseCommand - parse database command") {
		t.Fatalf("help error has no command context: %v", err)
	}
}

func TestRunHealthCommandWrapsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunHealthCommand(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readiness error = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "bootstrap.RunHealthCommand - readiness probe canceled") {
		t.Fatalf("readiness error has no function context: %v", err)
	}
}

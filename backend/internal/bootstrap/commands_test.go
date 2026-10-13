package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestCapacityUsesEveryRolePool(t *testing.T) {
	for key, value := range map[string]string{
		"FORCAD_API_DATABASE_MAX_CONNS": "16", "FORCAD_RECEIVER_DATABASE_MAX_CONNS": "32",
		"FORCAD_WORKER_DATABASE_MAX_CONNS": "16", "FORCAD_TICKER_DATABASE_MAX_CONNS": "8",
	} {
		t.Setenv(key, value)
	}
	var output bytes.Buffer
	if err := RuntimeCapacity(t.Context(), nil, &output); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Total int            `json:"database_connection_ceiling"`
		Roles map[string]int `json:"database_connections_by_role"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Total != 136 || report.Roles["receiver"] != 64 || report.Roles["ticker"] != 8 {
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

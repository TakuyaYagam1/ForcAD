package bootstrap

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
)

func TestPostgresPoolOptionsKeepRoleIdentityAndPoolerMode(t *testing.T) {
	cfg := config.Config{
		Role:               "worker",
		DatabaseURL:        "postgres://database/forcad_worker",
		DatabasePoolerAddr: "pooler:6432",
		DatabaseMaxConns:   24,
		DatabaseMinConns:   6,
		DatabaseTimeout:    7 * time.Second,
	}
	options := postgresPoolOptions(cfg)
	wantName := "forcad-worker"
	if instance, err := os.Hostname(); err == nil && instance != "" {
		wantName += "/" + instance
	}
	if len(wantName) > 63 {
		wantName = wantName[:63]
	}
	if options.URL != cfg.DatabaseURL || options.MaxConns != 24 || options.MinConns != 6 || options.Timeout != 7*time.Second ||
		options.ApplicationName != wantName || !options.TransactionPooling {
		t.Fatal("application storage settings were not preserved")
	}
	cfg.DatabasePoolerAddr = ""
	cfg.Role = strings.Repeat("x", 80)
	options = postgresPoolOptions(cfg)
	if options.TransactionPooling || options.ApplicationName != ("forcad-" + cfg.Role)[:63] {
		t.Fatal("direct mode or PostgreSQL application-name bound changed")
	}
}

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
)

func TestNewPoolRedactsMalformedDatabaseURL(t *testing.T) {
	const secret = "private-db-password"
	_, _, err := NewPool(context.Background(), config.Config{
		DatabaseURL: "postgres://forcad:" + secret + "@localhost/forcad%zz",
	})
	if err == nil {
		t.Fatal("NewPool() accepted a malformed PostgreSQL URL")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("NewPool() error exposed the password: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "postgres.NewPool - parse PostgreSQL connection settings:") {
		t.Fatalf("NewPool() error is missing its operation: %v", err)
	}
}

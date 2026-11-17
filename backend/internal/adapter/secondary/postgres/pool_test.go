package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewPoolRedactsMalformedDatabaseURL(t *testing.T) {
	const secret = "private-db-password"
	_, _, err := NewPool(context.Background(), PoolOptions{
		URL: "postgres://forcad:" + secret + "@localhost/forcad%zz",
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

func TestPoolSettingsPreserveConnectionAndTimeoutPolicy(t *testing.T) {
	for _, pooled := range []bool{false, true} {
		name := "direct"
		if pooled {
			name = "transaction pooler"
		}
		t.Run(name, func(t *testing.T) {
			settings, err := poolSettings(PoolOptions{
				URL:                "postgres://forcad:password@localhost/forcad?sslmode=disable&statement_timeout=1234&lock_timeout=17&idle_in_transaction_session_timeout=22",
				MaxConns:           24,
				MinConns:           6,
				Timeout:            7 * time.Second,
				ApplicationName:    "forcad-worker/instance",
				TransactionPooling: pooled,
			})
			if err != nil {
				t.Fatal(err)
			}
			if settings.MaxConns != 24 || settings.MinConns != 6 || settings.ConnConfig.ConnectTimeout != 7*time.Second ||
				settings.MaxConnLifetime != 30*time.Minute || settings.MaxConnLifetimeJitter != 5*time.Minute ||
				settings.MaxConnIdleTime != 5*time.Minute || settings.HealthCheckPeriod != 30*time.Second {
				t.Fatal("pool connection limits, lifetimes or timeout changed")
			}
			params := settings.ConnConfig.RuntimeParams
			if params["application_name"] != "forcad-worker/instance" {
				t.Fatal("application name was not passed through")
			}
			want := map[string]string{
				"statement_timeout":                   "7000",
				"lock_timeout":                        "2000",
				"idle_in_transaction_session_timeout": "10000",
			}
			if pooled {
				want = map[string]string{
					"statement_timeout":                   "1234",
					"lock_timeout":                        "17",
					"idle_in_transaction_session_timeout": "22",
				}
			}
			for key, value := range want {
				if params[key] != value {
					t.Fatalf("%s=%q, want %q", key, params[key], value)
				}
			}
		})
	}
}

package config

import (
	"strings"
	"testing"
)

func loadTest(values map[string]string) (Config, error) {
	return Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
}

func TestRuntimeBounds(t *testing.T) {
	for _, key := range []string{"DATABASE_MAX_CONNS", "WORKER_CONCURRENCY", "FLAG_RATE", "FLAG_BURST"} {
		t.Run(key, func(t *testing.T) {
			_, err := loadTest(map[string]string{"FORCAD_ROLE": "receiver", key: "0"})
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("expected bounded %s: %v", key, err)
			}
		})
	}
}

func TestDatabasePoolerKeepsCredentialsAndSeparatesRoles(t *testing.T) {
	for role, limit := range map[string]int32{"receiver": 32, "worker": 16, "ticker": 8} {
		c, err := loadTest(map[string]string{
			"FORCAD_ROLE": role, "POSTGRES_HOST": "database.example", "POSTGRES_DB": "custom",
			"POSTGRES_PASSWORD": "a@b/c?d#e", "DATABASE_POOLER_ADDR": "pgbouncer:6432",
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.DatabaseMaxConns != limit || !strings.Contains(c.DatabaseURL, "@pgbouncer:6432/forcad_"+role+"?") || !strings.Contains(c.DatabaseURL, "a%40b%2Fc%3Fd%23e") {
			t.Fatalf("incorrect pooler routing or limit for %s", role)
		}
	}
	c, err := loadTest(map[string]string{"FORCAD_ROLE": "receiver", "DATABASE_MAX_CONNS": "50", "FORCAD_RECEIVER_DATABASE_MAX_CONNS": "24"})
	if err != nil || c.DatabaseMaxConns != 24 {
		t.Fatalf("role override: limit=%d err=%v", c.DatabaseMaxConns, err)
	}
	if _, err := loadTest(map[string]string{"FORCAD_ROLE": "worker", "DATABASE_POOLER_ADDR": "missing-port"}); err == nil {
		t.Fatal("invalid pooler endpoint accepted")
	}
}

func TestDatabaseMinimumPoolSize(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings map[string]string
		want     int32
		invalid  bool
	}{
		{name: "default"},
		{name: "zero", settings: map[string]string{"DATABASE_MIN_CONNS": "0"}},
		{name: "partial", settings: map[string]string{"DATABASE_MIN_CONNS": "8"}, want: 8},
		{name: "full", settings: map[string]string{"DATABASE_MIN_CONNS": "32"}, want: 32},
		{name: "configured maximum", settings: map[string]string{"DATABASE_MAX_CONNS": "1000", "DATABASE_MIN_CONNS": "1000"}, want: 1000},
		{name: "negative", settings: map[string]string{"DATABASE_MIN_CONNS": "-1"}, invalid: true},
		{name: "above maximum", settings: map[string]string{"DATABASE_MIN_CONNS": "33"}, invalid: true},
		{name: "role maximum", settings: map[string]string{"DATABASE_MAX_CONNS": "50", "FORCAD_RECEIVER_DATABASE_MAX_CONNS": "24", "DATABASE_MIN_CONNS": "25"}, invalid: true},
		{name: "fraction", settings: map[string]string{"DATABASE_MIN_CONNS": "1.5"}, invalid: true},
		{name: "malformed", settings: map[string]string{"DATABASE_MIN_CONNS": "invalid"}, invalid: true},
		{name: "overflow", settings: map[string]string{"DATABASE_MIN_CONNS": "999999999999999999999"}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{"FORCAD_ROLE": "receiver"}
			for key, value := range test.settings {
				values[key] = value
			}
			cfg, err := loadTest(values)
			if test.invalid {
				if err == nil || !strings.Contains(err.Error(), "DATABASE_MIN_CONNS") {
					t.Fatalf("expected DATABASE_MIN_CONNS validation error, got %v", err)
				}
				return
			}
			if err != nil || cfg.DatabaseMinConns != test.want {
				t.Fatalf("minimum connections = %d, want %d; error: %v", cfg.DatabaseMinConns, test.want, err)
			}
		})
	}
}

func TestAdminCredentialsRequiredOnlyForAdminProcess(t *testing.T) {
	if _, err := loadTest(nil); err == nil {
		t.Fatal("admin API accepted empty credentials")
	}
	if _, err := loadTest(map[string]string{"FORCAD_ROLE": "receiver"}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretsAreEncodedAndNotReturnedInErrors(t *testing.T) {
	c, err := loadTest(map[string]string{"FORCAD_ROLE": "worker", "POSTGRES_PASSWORD": "a@b/c?d#e", "DATABASE_MAX_CONNS": "16"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.DatabaseURL, "a%40b%2Fc%3Fd%23e") {
		t.Fatal("password must be URL escaped")
	}
	_, err = loadTest(map[string]string{"FORCAD_ROLE": "worker", "DATABASE_URL": "secret://private-password"})
	if err == nil || strings.Contains(err.Error(), "private-password") {
		t.Fatal("invalid URL error exposed a secret")
	}
}

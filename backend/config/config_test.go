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

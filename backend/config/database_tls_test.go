package config

import (
	"net/url"
	"testing"
)

func TestVerifiedDatabaseTLSBothDirectAndPooler(t *testing.T) {
	for _, pooler := range []string{"", "pgbouncer:6432"} {
		values := map[string]string{"FORCAD_ROLE": "worker", "POSTGRES_HOST": "database.example", "POSTGRES_SSLMODE": "verify-full", "POSTGRES_SSLROOTCERT": "/run/ca.crt", "FORCAD_DATABASE_REQUIRE_TLS": "1", "DATABASE_POOLER_ADDR": pooler}
		cfg, err := Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
		if err != nil {
			t.Fatal(err)
		}
		address, err := url.Parse(cfg.DatabaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if address.Query().Get("sslmode") != "verify-full" || address.Query().Get("sslrootcert") != "/run/ca.crt" {
			t.Fatal("lost TLS verification settings")
		}
		if pooler != "" && address.Host != pooler {
			t.Fatal("TLS settings changed pooler routing")
		}
		values["DATABASE_URL"] = "postgres://runtime:secret@db/forcad?sslmode=disable"
		if _, err := Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }); err == nil {
			t.Fatal("secure deployment accepted an explicit insecure URL")
		}
		values["FORCAD_DATABASE_REQUIRE_TLS"] = "invalid"
		if _, err := Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }); err == nil {
			t.Fatal("invalid TLS requirement silently disabled verification")
		}
	}
}

package checker

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCheckerEnvironmentOmitsPlatformCredentials(t *testing.T) {
	source := []string{
		"PATH=/checker/bin", "CHECKER_SERVICE_TOKEN=checker-owned-value",
		"HOME=/checker", "SYSTEMROOT=C:\\Windows", "=C:=C:\\workspace",
		"DATABASE_URL=fixture-database", "database_url=duplicate-fixture",
		"POSTGRES_PASSWORD=fixture-postgres", "POSTGRES_RUNTIME_PASSWORD=fixture-runtime",
		"FORCAD_RUNTIME_DB_PASSWORD=fixture-provisioning",
		"PGBOUNCER_AUTH_FILE=fixture-auth", "NATS_PASSWORD=fixture-nats",
		"nats_user=fixture-nats-user", "NATS_TOKEN=fixture-nats-token",
		"RABBITMQ_DEFAULT_PASS=fixture-old-broker", "rabbitmq_password=fixture-old-password",
		"AMQP_URL=fixture-old-url", "BROKER_API_URL=fixture-old-management",
		"NATS_URL=fixture-nats-url", "REDIS_PASSWORD=fixture-redis",
		"REDIS_URL=fixture-redis-url", "ADMIN_PASSWORD=fixture-admin",
		"CONTROL_TOKEN=fixture-control", "FORCAD_CONFIG=/private/config.yml",
		"PGPASSWORD=fixture-libpq", "PGPASSFILE=/private/pgpass",
	}
	original := slices.Clone(source)
	want := slices.Clone(source[:5])
	if got := filterCheckerEnvironment(source); !slices.Equal(got, want) {
		t.Fatal("checker environment must keep only OS and checker-specific entries")
	}
	if !slices.Equal(source, original) {
		t.Fatal("filter modified the parent's environment slice")
	}
}

func TestPatchedCheckerEnvironmentPreservesPathWithoutPlatformSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "fixture-not-a-real-credential")
	t.Setenv("CHECKER_SERVICE_TOKEN", "checker-owned-value")
	checkerBin := filepath.Join(t.TempDir(), "checker-bin")
	for _, path := range []string{"", checkerBin} {
		env := patchedEnvironment(path)
		if environmentValue(env, "DATABASE_URL") != "" {
			t.Fatal("platform database connection reached checker environment")
		}
		if environmentValue(env, "CHECKER_SERVICE_TOKEN") != "checker-owned-value" {
			t.Fatal("checker-specific configuration was removed")
		}
		if path != "" {
			paths := filepath.SplitList(environmentValue(env, "PATH"))
			if len(paths) == 0 || paths[0] != checkerBin {
				t.Fatal("checker env_path did not precede the inherited PATH")
			}
		}
	}
	if os.Getenv("DATABASE_URL") != "fixture-not-a-real-credential" {
		t.Fatal("checker environment filtering changed the worker's environment")
	}
}

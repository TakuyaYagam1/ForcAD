package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/db"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
)

// Database isolates each test in a fresh schema, on an explicitly supplied
// database whose name ends with _test. It never truncates a shared database.
func Database(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	raw := os.Getenv("FORCAD_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("FORCAD_TEST_DATABASE_URL is not set")
	}
	address, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(address.Path, "_test") {
		t.Fatal("integration database name must end with _test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	schema := "test_" + hex.EncodeToString(id[:])
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
		_ = admin.Close(cleanup)
	})
	query := address.Query()
	query.Set("search_path", schema)
	address.RawQuery = query.Encode()
	if err := db.Migrate(ctx, address.String()); err != nil {
		t.Fatal(err)
	}
	pool, closePool, err := postgres.NewPool(ctx, config.Config{Role: "test", DatabaseURL: address.String(), DatabaseMaxConns: 16, DatabaseTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closePool)
	return pool, address.String()
}

package postgres_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/db"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/jackc/pgx/v5"
)

func TestTransactionPooler(t *testing.T) {
	address := os.Getenv("FORCAD_TEST_POOLER_ADDR")
	if address == "" {
		t.Skip("FORCAD_TEST_POOLER_ADDR is not set")
	}
	direct := os.Getenv("FORCAD_TEST_DATABASE_URL")
	parsed, err := url.Parse(direct)
	if err != nil || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("pooler tests require an isolated _test database")
	}
	ctx := t.Context()
	admin, err := pgx.Connect(ctx, direct)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(context.Background()) }()
	var existing *string
	if err := admin.QueryRow(ctx, `SELECT to_regclass('public.gameconfig')::text`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != nil {
		t.Fatal("refusing to reuse initialized public schema for pooler tests")
	}
	if err := db.Migrate(ctx, direct); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{"FORCAD_ROLE": "receiver", "DATABASE_URL": direct, "DATABASE_POOLER_ADDR": address, "DATABASE_MAX_CONNS": "16"}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := settings[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	pool, closePool, err := postgres.NewPool(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closePool()
	game := postgres.NewGameStore(pool)
	if _, err := game.Initialize(ctx, gameDocument(t)); err != nil {
		t.Fatal(err)
	}
	teams, tasks := gameEntityIDs(ctx, t, game)
	if _, err := pool.Exec(ctx, `UPDATE GameConfig SET game_running=TRUE,real_round=1,inflation=FALSE`); err != nil {
		t.Fatal(err)
	}
	life, err := game.Lifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f := submissionFixture{pool, postgres.New(pool), teams, tasks[0], life.Session.Generation}
	request := f.request(0, f.flags(t, 1, 1, 100))
	results, err := f.repo.SubmitBatch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}

	t.Run("pool statistics are available to the operator", func(t *testing.T) {
		settings, err := pgx.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			t.Fatal(err)
		}
		settings.Database = "pgbouncer"
		settings.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		stats, err := pgx.ConnectConfig(t.Context(), settings)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stats.Close(context.Background()) }()
		rows, err := stats.Query(t.Context(), "SHOW POOLS")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				t.Fatal(err)
			}
			found = found || values[0] == "forcad_receiver"
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("receiver pool is missing from statistics")
		}
	})

	t.Run("prepared statements survive backend reassignment", func(t *testing.T) {
		var workers sync.WaitGroup
		for i := range 16 {
			workers.Go(func() {
				for range 10 {
					var actual int
					if err := pool.QueryRow(t.Context(), `SELECT $1::integer + 1`, i).Scan(&actual); err != nil {
						t.Error(err)
						return
					}
					if actual != i+1 {
						t.Errorf("query result=%d", actual)
					}
				}
			})
		}
		workers.Wait()
	})
	t.Run("timeouts are enforced on server connections", func(t *testing.T) {
		for name, want := range map[string]string{"statement_timeout": "5s", "lock_timeout": "2s", "idle_in_transaction_session_timeout": "10s"} {
			var actual string
			if err := pool.QueryRow(t.Context(), `SELECT current_setting($1)`, name).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if actual != want {
				t.Fatalf("%s=%s, want %s", name, actual, want)
			}
		}
	})
	t.Run("receiver saturation leaves ticker available", func(t *testing.T) {
		first, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = first.Rollback(context.Background()) }()
		if _, err = first.Exec(t.Context(), `SELECT 1`); err != nil {
			t.Fatal(err)
		}
		second, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = second.Rollback(context.Background()) }()
		if _, err = second.Exec(t.Context(), `SELECT 1`); err != nil {
			t.Fatal(err)
		}
		deadline, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		defer cancel()
		if err := pool.Ping(deadline); err == nil {
			t.Fatal("receiver exceeded its two server connections")
		}
		settings["FORCAD_ROLE"] = "ticker"
		tickerConfig, err := config.Load(func(key string) (string, bool) { value, ok := settings[key]; return value, ok })
		if err != nil {
			t.Fatal(err)
		}
		ticker, closeTicker, err := postgres.NewPool(t.Context(), tickerConfig)
		if err != nil {
			t.Fatal(err)
		}
		defer closeTicker()
		if err := ticker.Ping(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

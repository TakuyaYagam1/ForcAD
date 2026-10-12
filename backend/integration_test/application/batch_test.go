package application_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

// This is a bounded concurrent correctness test, not a host throughput benchmark.
// Every request contains 100 different live flags and shares score rows with other
// requests. It exercises two receiver instances against the same database.
func TestTwentyTeamsSubmitBatchesAcrossReplicas(t *testing.T) {
	redisAddr := os.Getenv("FORCAD_TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("local Redis integration endpoint is not set")
	}
	pool, dsn := testutil.Database(t)
	var input strings.Builder
	input.WriteString(`game:
  start_time: "2050-01-01 00:00:00"
  round_time: 60
  flag_lifetime: 5
  inflation: false
tasks:
  - name: example
    checker: /fixture/checker
teams:
`)
	teamTokens := make([]string, 20)
	for team := 1; team <= 20; team++ {
		token := fmt.Sprintf("%016x", team)
		teamTokens[team-1] = token
		fmt.Fprintf(&input, "  - name: team-%d\n    ip: 192.0.2.%d\n    token: %q\n", team, team, token)
	}
	document, err := config.DecodeDocument(strings.NewReader(input.String()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.NewGameStore(pool).Initialize(t.Context(), document); err != nil {
		t.Fatal(err)
	}
	teamIDs := make(map[string]uuid.UUID, len(teamTokens))
	rows, err := pool.Query(t.Context(), `SELECT name,id FROM Teams`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		var id uuid.UUID
		if err = rows.Scan(&name, &id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		teamIDs[name] = id
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(teamIDs) != len(teamTokens) {
		t.Fatalf("loaded %d team UUIDs, want %d", len(teamIDs), len(teamTokens))
	}
	var taskID uuid.UUID
	if err = pool.QueryRow(t.Context(), `SELECT id FROM Tasks WHERE name='example'`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if taskID == uuid.Nil() {
		t.Fatal("loaded a nil task UUID")
	}
	if _, err = pool.Exec(t.Context(), `UPDATE GameConfig SET game_running=TRUE,real_round=1`); err != nil {
		t.Fatal(err)
	}
	repo := postgres.New(pool)
	batches := make([][]byte, 20)
	for team := range 20 {
		victimName := fmt.Sprintf("team-%d", (team+1)%20+1)
		victimID, ok := teamIDs[victimName]
		if !ok || victimID == uuid.Nil() {
			t.Fatalf("no UUID found for victim %q", victimName)
		}
		values := make([]string, 100)
		for item := range 100 {
			values[item] = fmt.Sprintf("%031d=", team*100+item)
			_, err := repo.Create(t.Context(), domain.Flag{Value: values[item], TeamID: victimID, TaskID: taskID, Round: 1}, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		batches[team], err = json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
	}
	servers := make([]*applicationProcess, 0, 2)
	for range 2 {
		values := map[string]string{"FORCAD_ROLE": "receiver", "DATABASE_URL": dsn,
			"REDIS_ADDR": redisAddr, "REDIS_DB": "15", "DATABASE_MAX_CONNS": "4"}
		servers = append(servers, startApplication(t, values))
	}
	client := &http.Client{Timeout: 20 * time.Second}
	for attempt := range 2 {
		start := make(chan struct{})
		latencies := make(chan time.Duration, 20)
		var requests sync.WaitGroup
		for team := range 20 {
			requests.Go(func() {
				<-start
				began := time.Now()
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, servers[(team+attempt)%2].URL+"/flags", bytes.NewReader(batches[team]))
				if err != nil {
					t.Error(err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Team-Token", teamTokens[team])
				response, err := client.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				defer response.Body.Close()
				body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
				if err != nil {
					t.Error(err)
					return
				}
				verdict := []byte("Flag accepted!")
				if attempt == 1 {
					verdict = []byte("Flag already stolen")
				}
				if response.StatusCode != 200 || bytes.Count(body, verdict) != 100 {
					t.Errorf("team %d attempt %d: HTTP %d, expected verdict count=%d", team+1, attempt, response.StatusCode, bytes.Count(body, verdict))
				}
				latencies <- time.Since(began)
			})
		}
		began := time.Now()
		close(start)
		requests.Wait()
		close(latencies)
		var samples []time.Duration
		for sample := range latencies {
			samples = append(samples, sample)
		}
		if len(samples) != 20 {
			t.Fatalf("only %d of 20 requests completed", len(samples))
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		t.Logf("20 teams x 100 flags, 2 receivers, 4 DB connections each, attempt=%d elapsed=%s p95=%s max=%s", attempt+1, time.Since(began), samples[18], samples[19])
	}
	var stolen, lost, records int
	var score float64
	if err := pool.QueryRow(t.Context(), `SELECT sum(stolen),sum(lost),sum(score),(SELECT count(*) FROM StolenFlags) FROM TeamTasks`).Scan(&stolen, &lost, &score, &records); err != nil {
		t.Fatal(err)
	}
	if stolen != 2000 || lost != 2000 || records != 2000 || math.Abs(score-50000) > 1e-6 {
		t.Fatalf("accounting: stolen=%d lost=%d records=%d total_score=%f", stolen, lost, records, score)
	}
}

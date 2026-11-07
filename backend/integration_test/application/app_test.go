package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	natsbroker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/nats"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestApplicationRunsPythonCheckersAndAcceptsBatchOnce(t *testing.T) {
	brokerURL, redisAddr := os.Getenv("FORCAD_TEST_NATS_URL"), os.Getenv("FORCAD_TEST_REDIS_ADDR")
	if brokerURL == "" || redisAddr == "" {
		t.Skip("local broker and Redis integration endpoints are not set")
	}
	prefix := os.Getenv("FORCAD_TEST_NATS_PREFIX")
	if !regexp.MustCompile(`^forcad_(test|e2e)_[A-Za-z0-9_]+$`).MatchString(prefix) {
		t.Fatal("integration broker requires an isolated FORCAD_TEST_NATS_PREFIX")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed for the checker protocol fixture")
	}
	pool, dsn := testutil.Database(t)
	directory := t.TempDir()
	checker := filepath.Join(directory, "checker.py")
	// The fixture stores flags beside itself and never contacts a service.
	source := "#!" + python + "\n" + `import pathlib, sys
action, host = sys.argv[1:3]
if action == "check":
    print("service smoke passed")
    sys.exit(101)
private, flag, place = sys.argv[3:6]
storage = pathlib.Path(__file__).parent / (host + "-" + private)
if action == "put":
    storage.write_text(flag)
    print(private)
    sys.exit(101)
if action == "get":
    if not storage.exists() or storage.read_text() != flag:
        print("flag mismatch")
        sys.exit(102)
    print("flag intact")
    sys.exit(101)
sys.exit(110)
`
	if err := os.WriteFile(checker, []byte(source), 0o700); err != nil {
		t.Fatal(err)
	}
	document, err := config.DecodeDocument(strings.NewReader(fmt.Sprintf(`game:
  mode: classic
  start_time: 2050-01-01 00:00:00
  round_time: 30
  rounds: 2
  flag_lifetime: 3
tasks:
  - name: example
    checker: %q
    checker_timeout: 3
    puts: 2
    gets: 1
    env_path: %q
teams:
  - name: attacker
    ip: 127.0.0.1
    token: "0000000000000001"
  - name: victim
    ip: 127.0.0.2
    token: "0000000000000002"
`, checker, filepath.Dir(python))))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewGameStore(pool).Initialize(t.Context(), document); err != nil {
		t.Fatal(err)
	}
	address, _ := url.Parse(dsn)
	stream := prefix + "_bootstrap_" + address.Query().Get("search_path")
	subject, consumer := stream+".jobs", "bootstrap-workers"
	values := map[string]string{
		"FORCAD_ROLE":        "all",
		"DATABASE_URL":       dsn,
		"REDIS_ADDR":         redisAddr,
		"NATS_URL":           brokerURL,
		"NATS_STREAM":        stream,
		"NATS_SUBJECT":       subject,
		"NATS_CONSUMER":      consumer,
		"ADMIN_USERNAME":     "organizer",
		"ADMIN_PASSWORD":     "local-integration-password",
		"CONTROL_TOKEN":      strings.Repeat("d", 32),
		"HTTP_ADDR":          "127.0.0.1:0",
		"METRICS_ADDR":       "127.0.0.1:0",
		"WORKER_CONCURRENCY": "4",
		"SHUTDOWN_TIMEOUT":   "5s",
	}
	t.Cleanup(func() {
		conn, err := natsgo.Connect(brokerURL, natsgo.Timeout(time.Second), natsgo.MaxReconnects(0))
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		js, err := jetstream.New(conn)
		if err != nil {
			t.Error(err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = js.DeleteStream(ctx, stream); err != nil {
			t.Error(err)
		}
	})
	app := startApplication(t, values)
	client := &http.Client{
		Timeout: 3 * time.Second,
	}
	var csrfToken string
	request := func(
		method, path string,
		body []byte,
		cookie *http.Cookie,
		token string,
	) (int, []byte, []*http.Cookie) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, app.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
			if method != http.MethodGet && strings.HasPrefix(path, "/api/admin/") {
				req.Header.Set("X-CSRF-Token", csrfToken)
			}
		}
		if token != "" {
			req.Header.Set("X-Team-Token", token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data, resp.Cookies()
	}
	status, body, cookies := request("POST", "/api/admin/login/", []byte(`{"username":"organizer","password":"local-integration-password"}`), nil, "")
	if status != 200 || len(cookies) == 0 {
		t.Fatalf("login: %d %s", status, body)
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(body, &session); err != nil || len(session.CSRFToken) != 64 {
		t.Fatal("login did not return a CSRF token")
	}
	csrfToken = session.CSRFToken
	status, body, _ = request("POST", "/api/admin/game/start_final/", []byte(`{"confirm":true,"generation":0}`), cookies[0], "")
	if status != 200 {
		t.Fatalf("start final: %d %s", status, body)
	}
	deadline := time.Now().Add(15 * time.Second)
	var flag string
	var finished int
	for time.Now().Before(deadline) {
		err = pool.QueryRow(t.Context(), `SELECT count(*) FROM CheckerJobs WHERE finished_at IS NOT NULL`).Scan(&finished)
		if err != nil {
			t.Fatal(err)
		}
		if finished == 2 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if finished != 2 {
		t.Fatal("two checker workflows did not complete")
	}
	if err = pool.QueryRow(t.Context(), `
		SELECT flag FROM Flags
		WHERE team_id = (SELECT id FROM Teams WHERE name = 'victim')
		LIMIT 1`).Scan(&flag); err != nil {
		t.Fatal(err)
	}
	batch := make([]string, 100)
	for i := range batch {
		batch[i] = flag
	}
	encoded, _ := json.Marshal(batch)
	status, body, _ = request("PUT", "/flags", encoded, nil, "0000000000000001")
	if status != 200 || bytes.Count(body, []byte("Flag accepted!")) != 1 || bytes.Count(body, []byte("Flag already stolen")) != 99 {
		t.Fatalf("flag batch: %d %s", status, body)
	}
	var stolen, flags, checks int
	if err = pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM StolenFlags),(SELECT count(*) FROM Flags),(SELECT sum(checks) FROM TeamTasks)`).Scan(&stolen, &flags, &checks); err != nil {
		t.Fatal(err)
	}
	if stolen != 1 || flags != 4 || checks != 2 {
		t.Fatalf("accounting: stolen=%d flags=%d checks=%d", stolen, flags, checks)
	}
	rows, err := pool.Query(t.Context(), `SELECT id FROM CheckerJobs`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	broker := natsbroker.New(natsbroker.Config{
		URL:           brokerURL,
		Stream:        stream,
		Subject:       subject,
		Consumer:      consumer,
		Concurrency:   1,
		MaxAckPending: 1024,
	}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := broker.Close(); err != nil {
			t.Error(err)
		}
	})
	if confirmed, err := broker.Publish(t.Context(), ids); err != nil || len(confirmed) != 2 {
		t.Fatalf("duplicate delivery: %v %v", confirmed, err)
	}
	status, body, _ = request("POST", "/api/admin/game/finish/", []byte(`{"confirm":true,"generation":1}`), cookies[0], "")
	if status != 200 {
		t.Fatalf("finish: %d %s", status, body)
	}
	readBoard := func() domain.Scoreboard {
		t.Helper()
		status, data, _ := request("GET", "/api/client/scoreboard/", nil, nil, "")
		if status != http.StatusOK {
			t.Fatalf("read scoreboard: HTTP %d", status)
		}
		var board domain.Scoreboard
		if err := json.Unmarshal(data, &board); err != nil {
			t.Fatal(err)
		}
		return board
	}
	deadline = time.Now().Add(3 * time.Second)
	board := readBoard()
	for time.Now().Before(deadline) && (board.State.Round != 1 || board.Runtime.Phase != "finished" || board.Runtime.ResultsPending) {
		time.Sleep(50 * time.Millisecond)
		board = readBoard()
	}
	if board.State.Round != 1 || board.Runtime.Phase != "finished" || board.Runtime.ResultsPending {
		t.Fatalf("final board: %+v", board)
	}
	status, _, _ = request("PUT", "/flags", encoded, nil, "0000000000000001")
	if status != 410 {
		t.Fatalf("finished game accepted flags: %d", status)
	}
	if err = pool.QueryRow(t.Context(), `SELECT count(*),(SELECT sum(checks) FROM TeamTasks) FROM Flags`).Scan(&flags, &checks); err != nil {
		t.Fatal(err)
	}
	if flags != 4 || checks != 2 {
		t.Fatalf("duplicate delivery repeated checkers: flags=%d checks=%d", flags, checks)
	}
	app.drain(t)
	statusRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, app.URL+"/api/client/status/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(statusRequest)
	if err == nil {
		_ = response.Body.Close()
		t.Fatalf("drained application still serves HTTP: %d", response.StatusCode)
	}
}

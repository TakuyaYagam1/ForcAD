package load_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type deployment struct {
	origin  string
	root    string
	compose []string
}

func openDeployment() (*deployment, error) {
	const location = "load.openDeployment"
	root, project, origin := os.Getenv("FORCAD_LOAD_ROOT"), os.Getenv("FORCAD_LOAD_PROJECT"), os.Getenv("FORCAD_LOAD_ORIGIN")
	address, err := url.Parse(origin)
	if err != nil || address.Scheme != "http" || address.Hostname() != "127.0.0.1" || address.Port() == "" || address.User != nil || address.Path != "" || address.RawQuery != "" {
		return nil, fmt.Errorf("%s - require an isolated loopback origin", location)
	}
	if !strings.HasPrefix(filepath.Base(root), "forcad-deployment.") || !strings.HasPrefix(project, "forcad-deployment-") {
		return nil, fmt.Errorf("%s - require an isolated deployment project", location)
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		return nil, fmt.Errorf("%s - locate repository: %w", location, err)
	}
	d := &deployment{origin: origin, root: root, compose: []string{"compose", "--project-name", project,
		"--project-directory", filepath.Join(root, "deployment/docker"),
		"-f", filepath.Join(root, "deployment/docker/compose.yml"),
		"-f", filepath.Join(repo, "deployment/docker/compose.e2e.yml"),
		"-f", filepath.Join(repo, "backend/load_test/compose.yml")}}
	switch os.Getenv("LOAD_DATABASE_STORAGE") {
	case "", "disk":
	case "memory":
		d.compose = append(d.compose, "-f", filepath.Join(repo, "backend/load_test/compose.memory.yml"))
	default:
		return nil, fmt.Errorf("%s - database storage must be disk or memory", location)
	}
	switch os.Getenv("LOAD_CONTAINER_LOGGING") {
	case "", "default":
	case "local":
		d.compose = append(d.compose, "-f", filepath.Join(repo, "backend/load_test/compose.logging.yml"))
	default:
		return nil, fmt.Errorf("%s - container logging must be default or local", location)
	}
	return d, nil
}

func (d *deployment) command(ctx context.Context, input string, args ...string) ([]byte, error) {
	const location = "load.deployment.command"
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, "docker", append(append([]string{}, d.compose...), args...)...)
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s - execute test command: %w: %s", location, err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func (d *deployment) query(ctx context.Context, sql string) ([]byte, error) {
	return d.command(ctx, sql, "exec", "-T", "postgres", "sh", "-ec", `exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1`)
}

func (d *deployment) number(ctx context.Context, sql string) (int64, error) {
	value, err := d.query(ctx, sql)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("load.deployment.number - decode count: %w", err)
	}
	return n, nil
}

func (d *deployment) start(ctx context.Context) error {
	const location = "load.deployment.start"
	count, err := d.number(ctx, `SELECT count(*) FROM Teams WHERE name LIKE 'team-%' AND ip LIKE '192.0.2.%'`)
	if err != nil {
		return err
	}
	if count != teamCount {
		return fmt.Errorf("%s - expected exactly 20 synthetic teams", location)
	}
	data, err := os.ReadFile(filepath.Join(d.root, "config/runtime/admin.env"))
	if err != nil {
		return fmt.Errorf("%s - read test credentials: %w", location, err)
	}
	credentials := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			credentials[key] = value
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("%s - create cookie jar: %w", location, err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	if err := d.post(ctx, client, "/api/admin/login/", map[string]any{"username": credentials["ADMIN_USERNAME"], "password": credentials["ADMIN_PASSWORD"]}); err != nil {
		return err
	}
	generation, err := d.number(ctx, `SELECT generation FROM GameSession`)
	if err != nil {
		return err
	}
	return d.post(ctx, client, "/api/admin/game/start_final/", map[string]any{"confirm": true, "generation": generation})
}

func (d *deployment) post(ctx context.Context, client *http.Client, path string, body any) error {
	const location = "load.deployment.post"
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s - encode request: %w", location, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.origin+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s - create request: %w", location, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", d.origin)
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s - send request: %w", location, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s - %s returned HTTP %d", location, path, response.StatusCode)
	}
	return nil
}

func sqlString(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
)

const (
	drainRequestTimeout = 5 * time.Second
	healthProbeTimeout  = 3 * time.Second
	maxCapacityReplicas = 1024
)

func commandConfig(path string) (config.Config, error) {
	fallback := map[string]string{"FORCAD_ROLE": "ticker"}
	if path != "" {
		document, err := config.LoadDocument(path)
		if err != nil {
			return config.Config{}, err
		}
		if document.Admin != nil {
			fallback["ADMIN_USERNAME"] = document.Admin.Username
			fallback["ADMIN_PASSWORD"] = document.Admin.Password
		}
		if document.Storages != nil {
			s := document.Storages
			fallback["POSTGRES_HOST"], fallback["POSTGRES_PORT"], fallback["POSTGRES_USER"], fallback["POSTGRES_PASSWORD"], fallback["POSTGRES_DB"] = s.DB.Host, strconv.Itoa(s.DB.Port), s.DB.User, s.DB.Password, s.DB.DBName
			fallback["REDIS_HOST"], fallback["REDIS_PORT"], fallback["REDIS_PASSWORD"], fallback["REDIS_DB"] = s.Redis.Host, strconv.Itoa(s.Redis.Port), s.Redis.Password, strconv.Itoa(s.Redis.DB)
			fallback["RABBITMQ_HOST"], fallback["RABBITMQ_PORT"], fallback["RABBITMQ_USER"], fallback["RABBITMQ_PASSWORD"], fallback["RABBITMQ_VHOST"] = s.RabbitMQ.Host, strconv.Itoa(s.RabbitMQ.Port), s.RabbitMQ.User, s.RabbitMQ.Password, s.RabbitMQ.VHost
		}
	}
	return config.Load(func(key string) (string, bool) {
		// Migrations and operator commands use the source PostgreSQL endpoint.
		// In particular, Goose's session advisory lock must bypass PgBouncer.
		if key == "DATABASE_POOLER_ADDR" {
			return "", true
		}
		if key == "FORCAD_ROLE" {
			return "ticker", true
		}
		if value, ok := os.LookupEnv(key); ok {
			return value, true
		}
		value, ok := fallback[key]
		return value, ok
	})
}

func RunDrainCommand(ctx context.Context, args []string, out io.Writer) error {
	const location = "bootstrap.RunDrainCommand"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - drain command canceled: %w", location, err)
	}
	options := flag.NewFlagSet("drain", flag.ContinueOnError)
	options.SetOutput(out)
	defaultEndpoint, err := controlEndpoint("/drain")
	if err != nil {
		return err
	}
	endpoint := options.String("url", defaultEndpoint, "local control endpoint")
	if err := options.Parse(args); err != nil {
		return fmt.Errorf("%s - parse drain command: %w", location, err)
	}
	if options.NArg() != 0 {
		return fmt.Errorf("%s - unexpected drain arguments", location)
	}
	token := os.Getenv("CONTROL_TOKEN")
	if token == "" {
		return fmt.Errorf("%s - CONTROL_TOKEN is required", location)
	}
	// #nosec G704 -- The operator supplies this control endpoint; redirects are disabled.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, *endpoint, nil)
	if err != nil {
		return fmt.Errorf("%s - create drain request", location)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: drainRequestTimeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	// #nosec G704 -- This CLI probes the configured control endpoint and refuses redirects.
	response, err := client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s - drain request canceled: %w", location, ctxErr)
		}
		return fmt.Errorf("%s - send drain request", location)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%s - drain returned HTTP %d", location, response.StatusCode)
	}
	_, err = fmt.Fprintln(out, "Drain requested")
	if err != nil {
		return fmt.Errorf("%s - write drain result: %w", location, err)
	}
	return nil
}

// RunHealthCommand is a container readiness probe with no external shell or
// network utility dependency. It never follows a redirect to another host.
func RunHealthCommand(ctx context.Context, args []string) error {
	const location = "bootstrap.RunHealthCommand"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - readiness probe canceled: %w", location, err)
	}
	if len(args) != 0 {
		return fmt.Errorf("%s - health does not accept arguments", location)
	}
	endpoint, err := controlEndpoint("/readyz")
	if err != nil {
		return err
	}
	// #nosec G704 -- The operator supplies this control endpoint; redirects are disabled.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%s - create readiness request: %w", location, err)
	}
	client := &http.Client{Timeout: healthProbeTimeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	// #nosec G704 -- This CLI probes the configured control endpoint and refuses redirects.
	response, err := client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s - readiness probe canceled: %w", location, ctxErr)
		}
		return fmt.Errorf("%s - readiness probe could not connect", location)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s - readiness returned HTTP %d", location, response.StatusCode)
	}
	return nil
}

func RunRuntimeCapacityCommand(ctx context.Context, args []string) error {
	return RuntimeCapacity(ctx, args, os.Stdout)
}

func RuntimeCapacity(ctx context.Context, args []string, out io.Writer) error {
	const location = "bootstrap.RuntimeCapacity"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - runtime capacity command canceled: %w", location, err)
	}
	options := flag.NewFlagSet("runtime-capacity", flag.ContinueOnError)
	options.SetOutput(out)
	apiReplicas := options.Int("api-replicas", 2, "API process count")
	receiverReplicas := options.Int("receiver-replicas", 2, "receiver process count")
	workerReplicas := options.Int("worker-replicas", 2, "worker process count")
	tickerReplicas := options.Int("ticker-replicas", 1, "scheduler process count")
	if err := options.Parse(args); err != nil {
		return fmt.Errorf("%s - parse runtime capacity command: %w", location, err)
	}
	if options.NArg() != 0 {
		return fmt.Errorf("%s - unexpected capacity arguments", location)
	}
	for _, n := range []int{*apiReplicas, *receiverReplicas, *workerReplicas, *tickerReplicas} {
		if n < 0 || n > maxCapacityReplicas {
			return fmt.Errorf("%s - replica count must be between 0 and 1024", location)
		}
	}
	cfg, err := commandConfig("")
	if err != nil {
		return err
	}
	report := struct {
		DatabaseConnectionCeiling int64            `json:"database_connection_ceiling"`
		DatabaseConnectionsByRole map[string]int64 `json:"database_connections_by_role"`
		CheckerProcessCeiling     int64            `json:"checker_process_ceiling"`
		CheckerMemoryCeilingMiB   int64            `json:"checker_memory_ceiling_mib"`
		HTTPInflightCeiling       int64            `json:"http_inflight_ceiling"`
		PerTeamFlagsPerSecond     int              `json:"per_team_flags_per_second"`
		PerTeamFlagBurst          int              `json:"per_team_flag_burst"`
		Note                      string           `json:"note"`
	}{
		DatabaseConnectionsByRole: make(map[string]int64),
		CheckerProcessCeiling:     int64(*workerReplicas) * int64(cfg.WorkerConcurrency),
		CheckerMemoryCeilingMiB:   int64(*workerReplicas) * int64(cfg.WorkerConcurrency) * int64(cfg.CheckerMemoryMB),
		HTTPInflightCeiling:       int64(*apiReplicas+*receiverReplicas) * int64(cfg.HTTPMaxInflight),
		PerTeamFlagsPerSecond:     cfg.FlagRate, PerTeamFlagBurst: cfg.FlagBurst,
		Note: "Configured ceilings, not measured throughput or reserved RAM. Include database, runtime and operating system overhead in the host budget.",
	}
	for role, replicas := range map[string]int{"api": *apiReplicas, "receiver": *receiverReplicas, "worker": *workerReplicas, "ticker": *tickerReplicas} {
		limit, err := config.DatabasePoolSize(role, os.LookupEnv)
		if err != nil {
			return err
		}
		report.DatabaseConnectionsByRole[role] = int64(replicas) * int64(limit)
		report.DatabaseConnectionCeiling += int64(replicas) * int64(limit)
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("%s - encode runtime capacity: %w", location, err)
	}
	return nil
}

func controlEndpoint(route string) (string, error) {
	const location = "bootstrap.controlEndpoint"
	address := os.Getenv("METRICS_ADDR")
	if address == "" {
		address = ":9090"
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("%s - parse metrics address: %w", location, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + route, nil
}

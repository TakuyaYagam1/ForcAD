package load_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WaitDelay cannot release a caller blocked in Scan while a descendant retains
// stdout. Closing our reader on cancellation makes that shutdown bounded too.
func collectDiagnosticLines(ctx context.Context, cmd *exec.Cmd, consume func(string) error) error {
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	stopClose := context.AfterFunc(ctx, func() {
		_ = output.Close()
	})
	defer stopClose()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var consumeErr error
	for scanner.Scan() {
		if consumeErr = consume(scanner.Text()); consumeErr != nil {
			break
		}
	}
	if consumeErr != nil || scanner.Err() != nil {
		_ = cmd.Process.Kill()
	}
	return errors.Join(consumeErr, scanner.Err(), cmd.Wait())
}

// One persistent psql session samples short lock convoys without starting a
// Docker process per sample. Only statement names are recorded, never values.
func (o *observers) sampleDatabase(ctx context.Context, d *deployment, directory string) {
	const location = "load.observers.sampleDatabase"
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	file, err := os.OpenFile(filepath.Join(directory, "database.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		o.fail(fmt.Errorf("%s - create samples: %w", location, err))
		return
	}
	defer file.Close()
	ids, err := d.command(ctx, "", "ps", "--status", "running", "-q", "postgres")
	if err != nil {
		if ctx.Err() == nil {
			o.fail(fmt.Errorf("%s - locate PostgreSQL: %w", location, err))
		}
		return
	}
	container := strings.TrimSpace(string(ids))
	if len(container) != 64 || strings.IndexFunc(container, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	}) != -1 {
		o.fail(fmt.Errorf("%s - require one full PostgreSQL container ID", location))
		return
	}
	// The Compose plugin can outlive docker.exe on Windows. Use direct exec and
	// tag this session so cleanup also stops the server-side work after detach.
	application := "forcad-load-database-" + rand.Text()
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()
		cmd := exec.CommandContext(cleanup, "docker", "exec", "-i", container, "sh", "-ec",
			`exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1`)
		cmd.WaitDelay = time.Second
		cmd.Stdin = strings.NewReader("SELECT pg_terminate_backend(pid, 1000) FROM pg_stat_activity WHERE application_name = " + sqlString(application) + ";\nSELECT count(*) FROM pg_stat_activity WHERE application_name = " + sqlString(application) + ";\n")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Fields(string(output))
			if len(lines) == 0 || lines[len(lines)-1] != "0" {
				err = errors.New("sampler database session remains active")
			}
		}
		if err != nil {
			o.fail(fmt.Errorf("%s - stop sampler session: %w", location, err))
		}
	}()
	args := []string{"exec", "-i", "--env", "PGAPPNAME=" + application, container, "sh", "-ec",
		`exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1`}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(`SELECT json_build_object('time', clock_timestamp(),
'wal_bytes',(SELECT wal_bytes FROM pg_stat_wal),
'wal_fsyncs',(SELECT sum(fsyncs) FROM pg_stat_io WHERE object='wal' AND context='normal'),
'checkpoint',(SELECT row_to_json(checkpoint) FROM pg_stat_checkpointer AS checkpoint),
'sessions', COALESCE(json_agg(json_build_object(
'pid',pid,'role',application_name,'backend_type',backend_type,'state',state,'wait_type',wait_event_type,'wait',wait_event,
'transaction_age',extract(epoch FROM clock_timestamp()-xact_start),
'query_age',extract(epoch FROM clock_timestamp()-query_start),'blockers',pg_blocking_pids(pid),
'query',CASE WHEN query LIKE '-- name:%' THEN split_part(query,E'\n',1)
WHEN query IN ('begin','commit','rollback') THEN query ELSE 'other' END)),'[]'))
FROM pg_stat_activity WHERE pid<>pg_backend_pid() AND (
    (backend_type='client backend' AND state<>'idle') OR
    (backend_type IN ('checkpointer','background writer','autovacuum worker')
     AND wait_event_type IS DISTINCT FROM 'Activity'))
\watch 0.25
`)
	err = collectDiagnosticLines(ctx, cmd, func(line string) error {
		if strings.HasPrefix(line, "{") {
			_, err := fmt.Fprintln(file, line)
			return err
		}
		return nil
	})
	if ctx.Err() == nil {
		if err == nil {
			err = errors.New("sampler exited before cancellation")
		}
		o.fail(fmt.Errorf("%s - sample PostgreSQL: %w", location, err))
	}
}

func flagLogTimeout(elapsed time.Duration) time.Duration {
	return max(2*time.Minute, elapsed/10)
}

func saveFlagLog(
	ctx context.Context,
	d *deployment,
	directory string,
	elapsed time.Duration,
) error {
	const location = "load.saveFlagLog"
	// A continuous high-rate run can produce millions of unrelated access lines.
	// Stream them with bounded memory, scaling the final scan budget with the run.
	ctx, cancel := context.WithTimeout(ctx, flagLogTimeout(elapsed))
	defer cancel()
	file, err := os.OpenFile(filepath.Join(directory, "flag.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("%s - create log: %w", location, err)
	}
	defer file.Close()
	args := append(append([]string{}, d.compose...), "logs", "--no-color", "--timestamps", "receiver", "nginx")
	cmd := exec.CommandContext(ctx, "docker", args...)
	err = collectDiagnosticLines(ctx, cmd, func(line string) error {
		if strings.Contains(line, "/flags") {
			_, err := fmt.Fprintln(file, line)
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s - collect flag logs: %w", location, err)
	}
	return nil
}

type poolMetricReplica struct {
	role, index string
}

func poolMetricReplicas() ([]poolMetricReplica, error) {
	const location = "load.poolMetricReplicas"
	var replicas []poolMetricReplica
	for _, role := range []string{"receiver", "api"} {
		// test_deployment.sh --load uses these settings, default 2, bounds 1..16.
		name := "LOAD_" + strings.ToUpper(role) + "_REPLICAS"
		count := 2
		if raw := os.Getenv(name); raw != "" {
			var err error
			count, err = strconv.Atoi(raw)
			if err != nil || count < 1 || count > 16 {
				return nil, fmt.Errorf("%s - %s must be an integer between 1 and 16", location, name)
			}
		}
		for index := 1; index <= count; index++ {
			replicas = append(replicas, poolMetricReplica{role, strconv.Itoa(index)})
		}
	}
	return replicas, nil
}

func keepPoolMetric(line string) bool {
	if strings.HasPrefix(line, "forcad_database_") || strings.HasPrefix(line, "forcad_redis_pool_") || strings.HasPrefix(line, "forcad_flag_submission_phase_duration_seconds") || strings.HasPrefix(line, "forcad_websocket_") {
		return true
	}
	name := line
	if index := strings.IndexAny(line, "{ \t"); index >= 0 {
		name = line[:index]
	}
	switch name {
	case "forcad_redis_connections", "forcad_redis_connections_idle",
		"go_goroutines", "forcad_log_dropped_total",
		"go_gc_duration_seconds", "go_gc_duration_seconds_sum", "go_gc_duration_seconds_count",
		"go_threads", "go_memstats_heap_alloc_bytes", "go_memstats_heap_objects",
		"go_memstats_heap_inuse_bytes", "go_gc_cpu_fraction", "go_sched_gomaxprocs_threads":
		return true
	default:
		return false
	}
}

func (o *observers) samplePool(ctx context.Context, d *deployment, directory string) {
	const location = "load.observers.samplePool"
	replicas, err := poolMetricReplicas()
	if err != nil {
		o.fail(err)
		return
	}
	scrape, closeScraper, err := newPoolMetricScraper(ctx, d, replicas)
	if err != nil {
		if ctx.Err() == nil {
			o.fail(err)
		}
		return
	}
	defer closeScraper()
	file, err := os.OpenFile(filepath.Join(directory, "pool.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		o.fail(fmt.Errorf("%s - create samples: %w", location, err))
		return
	}
	defer file.Close()
	for ctx.Err() == nil {
		for _, replica := range replicas {
			data, err := scrape(ctx, replica)
			if err != nil {
				if ctx.Err() == nil {
					o.fail(err)
				}
				return
			}
			if err := writePoolMetricSample(file, time.Now().UTC(), replica, data); err != nil {
				o.fail(err)
				return
			}
		}
		if !waitContext(ctx, time.Second) {
			return
		}
	}
}

func writePoolMetricSample(
	output io.Writer,
	now time.Time,
	replica poolMetricReplica,
	data []byte,
) error {
	const location = "load.writePoolMetricSample"
	buffer := bufio.NewWriterSize(output, 16<<10)
	if _, err := fmt.Fprintf(buffer, "%s %s-%s\n", now.UTC().Format(time.RFC3339Nano), replica.role, replica.index); err != nil {
		return fmt.Errorf("%s - write sample header: %w", location, err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if keepPoolMetric(line) {
			if _, err := fmt.Fprintln(buffer, line); err != nil {
				return fmt.Errorf("%s - write metric: %w", location, err)
			}
		}
	}
	if err := buffer.Flush(); err != nil {
		return fmt.Errorf("%s - flush sample: %w", location, err)
	}
	return nil
}

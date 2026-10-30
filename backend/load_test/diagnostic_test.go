package load_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
	stopClose := context.AfterFunc(ctx, func() { _ = output.Close() })
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
	if len(container) != 64 || strings.IndexFunc(container, func(r rune) bool { return (r < '0' || r > '9') && (r < 'a' || r > 'f') }) != -1 {
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

func saveFlagLog(ctx context.Context, d *deployment, directory string, elapsed time.Duration) error {
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

func (o *observers) samplePool(ctx context.Context, d *deployment, directory string) {
	const location = "load.observers.samplePool"
	apiReplicas := 2
	if raw := os.Getenv("LOAD_API_REPLICAS"); raw != "" {
		var err error
		apiReplicas, err = strconv.Atoi(raw)
		if err != nil || apiReplicas < 1 || apiReplicas > 16 {
			o.fail(fmt.Errorf("%s - LOAD_API_REPLICAS must be an integer between 1 and 16", location))
			return
		}
	}
	replicas := []struct{ role, index string }{{"receiver", "1"}, {"receiver", "2"}}
	for index := 1; index <= apiReplicas; index++ {
		replicas = append(replicas, struct{ role, index string }{"api", strconv.Itoa(index)})
	}
	file, err := os.OpenFile(filepath.Join(directory, "pool.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		o.fail(fmt.Errorf("%s - create samples: %w", location, err))
		return
	}
	defer file.Close()
	for ctx.Err() == nil {
		for _, replica := range replicas {
			data, err := d.command(ctx, "", "exec", "-T", "--index", replica.index, replica.role, "wget", "-qO-", "http://127.0.0.1:9090/metrics")
			if err != nil {
				if ctx.Err() == nil {
					o.fail(err)
				}
				return
			}
			if _, err := fmt.Fprintf(file, "%s %s-%s\n", time.Now().UTC().Format(time.RFC3339Nano), replica.role, replica.index); err != nil {
				o.fail(fmt.Errorf("%s - write sample header: %w", location, err))
				return
			}
			for line := range strings.SplitSeq(string(data), "\n") {
				if strings.HasPrefix(line, "forcad_database_") || strings.HasPrefix(line, "forcad_redis_pool_") || strings.HasPrefix(line, "forcad_websocket_") || strings.HasPrefix(line, "go_goroutines ") || strings.HasPrefix(line, "forcad_log_dropped_total ") {
					if _, err := fmt.Fprintln(file, line); err != nil {
						o.fail(fmt.Errorf("%s - write metric: %w", location, err))
						return
					}
				}
			}
		}
		if !waitContext(ctx, time.Second) {
			return
		}
	}
}

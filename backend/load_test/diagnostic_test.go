package load_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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
	args := append(append([]string{}, d.compose...), "exec", "-T", "postgres", "sh", "-ec",
		`exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1`)
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
	output, err := cmd.StdoutPipe()
	if err != nil {
		o.fail(fmt.Errorf("%s - open sample stream: %w", location, err))
		return
	}
	if err := cmd.Start(); err != nil {
		o.fail(fmt.Errorf("%s - start sampling: %w", location, err))
		return
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "{") {
			if _, err := fmt.Fprintln(file, scanner.Text()); err != nil {
				o.fail(fmt.Errorf("%s - write sample: %w", location, err))
				cancel()
				break
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		o.fail(fmt.Errorf("load.observers.sampleDatabase - read samples: %w", err))
		cancel()
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		o.fail(fmt.Errorf("load.observers.sampleDatabase - sample PostgreSQL: %w", err))
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
	output, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s - open log stream: %w", location, err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s - read container logs: %w", location, err)
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var writeErr error
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "/flags") {
			if _, writeErr = fmt.Fprintln(file, scanner.Text()); writeErr != nil {
				cancel()
				break
			}
		}
	}
	if scanner.Err() != nil {
		cancel()
	}
	if err := errors.Join(writeErr, scanner.Err(), cmd.Wait()); err != nil {
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

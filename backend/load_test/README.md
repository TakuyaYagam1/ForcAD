# Load testing

`make -C backend test-load` runs the harness against an isolated local Compose
stack through Nginx. It requires Linux or WSL, Bash, Make, Go, Python 3, and
Docker Compose. The harness removes its test containers, images, and volumes on
exit.

The workload uses 20 teams, three fixture services, six REST routes, active
Python checkers, and 20 WebSocket observers. Flag traffic sends 40 batches per
second with 100 values per batch, including duplicates. The stack runs two
receivers and two workers with eight checker slots each.

Set `LOAD_TEAM_COUNT=28 LOAD_WORKER_REPLICAS=20` to check 20 worker replicas
against 28 synthetic teams. This retains three fixture services and one PUT
and GET per service by default. It verifies the worker pool and result
persistence, but does not model the final services.
Set `LOAD_PUTS=10,12,13` to exercise 35 synthetic PUT actions per team per
round across the three fixtures, with matching possible place counts. Places
are covered once before any place is reused. The fixture behavior and latency
still differ from the final checkers.
Set `LOAD_ROUND_TIME=60 LOAD_CHECKER_TIMEOUT=20` for a final-style schedule;
the default fast harness uses a 10-second round and 5-second action timeout.
Use `LOAD_PROFILE=checker` to run the worker-focused check with two flag batches
per second and without the flag burst. It checks that all fixture checker
results stay at 101 across the run. It does not replace the separate flag
submission capacity test.

## Run

From the repository root:

```sh
make -C backend test-load                                       # smoke: 500 RPS for 30s
LOAD_PROFILE=stress LOAD_DIAGNOSTICS=1 make -C backend test-load # 500 to 10000 RPS
```

For the 28-team synthetic checker run (20 workers, eight slots each):

```sh
LOAD_TEAM_COUNT=28 LOAD_WORKER_REPLICAS=20 LOAD_WORKER_CONCURRENCY=8 \
LOAD_PUTS=10,12,13 LOAD_ROUND_TIME=60 LOAD_CHECKER_TIMEOUT=20 \
LOAD_PROFILE=checker LOAD_REST_RPS=50 LOAD_DURATION=10m \
LOAD_CHECKPOINT_INTERVAL=5m LOAD_DIAGNOSTICS=1 make -C backend test-load
```

After a passing preflight, repeat with `LOAD_DURATION=5h`. Each full round
must contain 84 UP results and 980 stored flags. Reports retain the first
100 failed jobs and actions, including runner errors and action timestamps.
Worker logs also retain bounded process diagnostics on checker shutdown: PID,
signal results, Linux process state, and a Go stack if cleanup cannot reap the
process. Correlate these records by job ID; checker arguments and flag data are
excluded. A diagnostic rerun alone does not establish that a failure is fixed.

The historical 10,000 REST RPS stages below used four API replicas and:

```sh
GOMAXPROCS=32 \
LOAD_PROFILE=load LOAD_REST_RPS=10000 \
LOAD_DURATION=15m LOAD_CHECKPOINT_INTERVAL=15m \
LOAD_API_REPLICAS=4 LOAD_WORKER_CONCURRENCY=8 \
LOAD_API_DATABASE_MAX_CONNS=16 LOAD_API_DATABASE_MIN_CONNS=16 \
LOAD_API_HTTP_MAX_INFLIGHT=256 LOAD_API_GOGC=200 \
LOAD_PGBOUNCER_API_CONNECTIONS=32 LOAD_PGBOUNCER_API_MIN_CONNECTIONS=32 \
LOAD_DATABASE_STORAGE=disk LOAD_CONTAINER_LOGGING=default LOAD_DIAGNOSTICS=1 \
make -C backend test-load
```

For a five-minute repeat, set both durations to `5m`. For a five-hour run, use
`LOAD_PROFILE=soak` and set both to `5h`. Without a checkpoint override, steady
profiles pause for reconciliation every five minutes. Warmup, recovery, and
final checks add time. See [profile_test.go](profile_test.go) and
[compose.yml](compose.yml) for other settings.

`LOAD_API_DATABASE_MAX_CONNS=16` applies to each API replica: the default two
replicas allow 32 PgBouncer client connections; four allow 64. PgBouncer's API
pool separately allows 32 PostgreSQL connections. HTTP RPS is not SQL RPS:
scoreboard reads use a 500 ms cache, while other routes perform database reads.
Size pools using acquisition waits, cancellations and PgBouncer queue metrics.

The current Nginx configuration limits REST traffic to 150 requests/s per IP
(burst 60); the archived 2026-10-13 runs predate that limit. This harness uses
Nginx too, so the historical command does not establish current end-to-end
capacity. Flag submissions have a separate 200 values/s budget per team
(burst 400), including all values in a batch; 4,000 values/s across 20 teams
is 40 full batches/s, not 4,000 HTTP requests/s.

## Recorded runs on 2026-10-13

The rows are separate continuous 10,000 REST RPS stages using the configuration
above. The host was an Intel i9-13900KF with 32 GB RAM, running Windows with
WSL2 limited to 28 GB and 32 logical CPUs. PostgreSQL used disk storage, with
diagnostics enabled.

| Duration | REST requests | Capacity 503 | REST p99 | Result |
| --- | ---: | ---: | ---: | --- |
| 15 min | 8,999,989 | 0 | 12.99 ms | PASS |
| 5 min repeat | 3,000,000 | 0 | 16.17 ms | PASS |
| 5 hours | 180,000,000 | 2,653 (0.001474%) | 11.18 ms | FAIL: strict zero-error criterion |

All ordinary flag batches succeeded in all three runs. The five-hour run
processed 720,000 batches (p99 33.29 ms); all 2,041,676 accepted steals matched
the database. WebSocket observations and recovery passed. The harness skipped
final checker-history checks after the five-hour REST failure. Its REST success
rate was 99.998526%.

Most 503s occurred in one short burst while Windows had 221 to 404 MiB
available and 99% CPU. Interactive host activity may have contributed, but the
run did not establish the cause. Repeat on an idle host to isolate its effect.

Reports include `generator_valid`, `slo_passed`, latency, errors, and flag
reconciliation. A `stress` run allows REST 429 and 503 responses, so a passing
command alone does not establish capacity. Results: [short runs](results/RESULTS-13900KF.txt),
[five-hour results](results/20261013-112709-soak/RESULTS.txt), and the
[five-hour report](results/20261013-112709-soak/latest-report.json).

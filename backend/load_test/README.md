# Load testing

`make -C backend test-load` runs the harness against an isolated local Compose
stack through Nginx. It requires Linux or WSL, Bash, Make, Go, Python 3, and
Docker Compose. The harness removes its test containers, images, and volumes on
exit.

The workload uses 20 teams, three fixture services, six REST routes, active
Python checkers, and 20 WebSocket observers. Flag traffic sends 40 batches per
second with 100 values per batch, including duplicates. The stack runs two
receivers and two workers with eight checker slots each.

## Run

From the repository root:

```sh
make -C backend test-load                                       # smoke: 500 RPS for 30s
LOAD_PROFILE=stress LOAD_DIAGNOSTICS=1 make -C backend test-load # 500 to 10000 RPS
```

To reproduce the 10,000 REST RPS stages below, use four API replicas and:

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

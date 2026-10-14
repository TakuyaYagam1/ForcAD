# Load testing

Vegeta tests an isolated local Compose stack through Nginx. Requires Linux/WSL,
Bash, Make, Go, Python 3 and Docker Compose. Run from the repository root;
test containers and volumes are removed on exit.

## Workload

20 teams, three fixture services, six REST routes, active Python checkers and
20 WebSocket clients. Flag traffic: 40 batches/s x 100 values, including duplicates.
Two receivers and two workers with eight checker slots each share the host with
the application, database and generator.

## Run

```sh
make -C backend test-load                                      # smoke: 500 RPS, 30s
LOAD_PROFILE=stress LOAD_DIAGNOSTICS=1 make -C backend test-load # 500 -> 10000 RPS
```

Configuration used for the 10k runs below (four API replicas):

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

For a five-minute repeat, set both durations to `5m`. For a continuous five-hour
run, use `LOAD_PROFILE=soak` and set both to `5h`. Without a checkpoint override,
steady profiles pause for reconciliation every five minutes. Warmup, recovery
and final checks take additional time. Other profiles/settings: [profile_test.go](profile_test.go)
and [compose.yml](compose.yml).

## Recorded runs - 2026-10-13

Intel i9-13900KF / 32 GB RAM, Windows + WSL2 (32 logical CPUs, 28 GB RAM limit).
PostgreSQL on disk, diagnostics enabled. All rows are separate continuous
**10000 REST RPS** stages using the configuration above.

| Duration | REST requests | Capacity 503 | REST p99 | Result |
| --- | ---: | ---: | ---: | --- |
| 15 min | 8,999,989 | 0 | 12.99 ms | PASS |
| 5 min repeat | 3,000,000 | 0 | 16.17 ms | PASS |
| 5 hours | 180,000,000 | 2,653 (0.001474%) | 11.18 ms | FAIL: zero-error criterion |

All ordinary flag batches succeeded in all three runs. The five-hour run processed
720,000 batches (p99 33.29 ms); all 2,041,676 accepted steals matched the DB.
WebSocket observations and recovery passed. Final checker-history checks were
skipped by the harness after the five-hour REST failure.

Most 503s occurred in one short burst coinciding with host memory pressure:
Windows had 221-404 MiB available and 99% CPU. Interactive use of the PC, including
possible VS Code startup, may have contributed; an idle-host repeat is needed to
isolate its impact. Five-hour REST success was **99.998526%**.

Reports contain `generator_valid`, `slo_passed`, latency, errors and flag reconciliation.
`stress` allows REST 429/503, so test exit alone does not establish capacity.
Results: [short runs](results/RESULTS-13900KF.txt),
[five-hour results](results/20261013-112709-soak/RESULTS.txt),
[five-hour report](results/20261013-112709-soak/latest-report.json).

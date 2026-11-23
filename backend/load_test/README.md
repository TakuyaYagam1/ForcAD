# Load testing

Use this harness to measure a local, isolated ForcAD stack through Nginx. It requires Bash, Make, Go, Python 3, Docker, and the Compose plugin. The wrapper supports Linux/WSL and MINGW/MSYS/Cygwin with Docker Desktop on Windows. Its exit trap attempts to remove the temporary project, containers, images, volumes, and files; check for leftovers if the process is interrupted.

From the repository root:

```sh
make -C backend test-load
LOAD_PROFILE=stress LOAD_DIAGNOSTICS=1 make -C backend test-load
LOAD_PROFILE=checker make -C backend test-load
```

The default `smoke` profile measures a 30-second steady stage at 500 REST requests/s. `stress` runs one-minute stages at 500, 1,000, 2,500, 5,000, and 10,000 REST requests/s. `checker` runs a 90-second steady stage with two flag batches/s and no overload burst. Warmup and recovery stages add time. See [profile_test.go](profile_test.go) for profiles and controls, and [compose.yml](compose.yml) for the isolated stack.

The default workload uses 20 synthetic teams, three fixture services, six REST routes, Python checkers, and 20 WebSocket observers. The stack has two receivers and two workers with eight checker slots each. Steady stages send 40 flag batches/s, each with 100 values including duplicates: 4,000 values/s, not 4,000 HTTP requests/s. The fixtures do not model the final services. The checker profile exercises worker and result persistence; it does not measure flag-submission capacity.

`make -C backend test-load` enables `LOAD_CLIENT_BUCKETS=1` by default. The isolated adapter models separate clients while retaining the production 150 requests/s per-IP limit. Each run probes a source without the synthetic header at 120 requests/s (accepted) and 300 requests/s (HTTP 429 with `Retry-After`). Disabling the adapter makes the default 500 requests/s from one source exceed the per-IP limit. Current Nginx also limits REST bursts to 60; flag submissions have a separate budget of 200 values/s per team with a burst of 500.

## Measurement

Open `backend/load_test/reports/<run>/report.json` for the current run. Set `LOAD_REPORT_DIR` to an absolute path to keep reports elsewhere.

Reports distinguish requested load from actual delivery. `generator_valid` requires at least 95% delivery and p99 lateness of at most 100 ms against scheduled starts. The normal SLO also requires zero HTTP errors and response p99 of at most one second. Overload stages do not enforce that SLO; `stress` allows REST 429 and 503 responses, so a passing command alone does not establish capacity. `mean_ms` is Vegeta's full hit duration; `result_consumer_delay_*` measures the additional delay before the harness receives a completed hit.

The 10,000 REST requests/s runs recorded on 2026-10-13 used the old RabbitMQ transport, four API replicas, two receivers, two workers with eight slots each, disk-backed PostgreSQL, and diagnostics. The host was an Intel i9-13900KF with 32 logical CPUs and 32 GB RAM; WSL2 had a 28 GB limit. The generator, services, database, and checkers shared that host.

| Run | REST requests | Capacity 503 | REST p99 | Outcome |
| --- | ---: | ---: | ---: | --- |
| 15 minutes | 8,999,989 | 0 | 12.99 ms | Pass |
| 5-minute repeat | 3,000,000 | 0 | 16.17 ms | Pass |
| 5 hours | 180,000,000 | 2,653 (0.001474%) | 11.18 ms | Failed zero-error requirement |

The five-hour run recorded 180,000,000 HTTP responses: 179,997,347 were successful (99.998526%), and 2,653 were capacity HTTP 503 responses, not generator-delivery failures. All 720,000 flag batches succeeded (p99 33.29 ms), and all 2,041,676 accepted flag values matched the database. WebSocket observations and recovery passed. Final checker-history validation was skipped after the REST failure. These results describe the stated host and configuration; they are not a capacity guarantee. See the [short-run results](results/RESULTS-13900KF.txt), [five-hour report](results/20261013-112709-soak/RESULTS.txt), and [machine-readable report](results/20261013-112709-soak/latest-report.json).

The historical runs predate the current Nginx per-IP REST limit and use a different job transport. Compare a new run with the same host, configuration, and source before drawing conclusions.

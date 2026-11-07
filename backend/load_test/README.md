# Load testing

`make -C backend test-load` runs the harness against an isolated local Compose
stack through Nginx. It requires Linux or WSL, Bash, Make, Go, Python 3, and
Docker Compose. The harness removes its test containers, images, and volumes on
exit.

The workload uses 20 teams, three fixture services, six REST routes, active
Python checkers, and 20 WebSocket observers. Flag traffic sends 40 batches per
second with 100 values per batch, including duplicates. The stack runs two
receivers and two workers with eight checker slots each.

Checker jobs use persistent NATS JetStream. `LOAD_BROKER_PREFLIGHT=1` verifies
worker drain and recovery after restarting only the isolated stack's NATS
container before starting measured traffic. Its test streams use a separate
namespace from application jobs. When `LOAD_REPORT_DIR` is set, the full redacted
preflight output is saved as `broker-preflight.jsonl` beside the run artifacts.
Historical RabbitMQ results describe the old
transport and must be compared with a new run before claiming an improvement.

`LOAD_WORKER_CPU_SHARES` sets relative CPU weight; default `0` inherits Docker settings, and idle CPU remains available to all services.
`LOAD_API_CPU_SHARES` sets API replicas' relative CPU weight (default `0`); verify actual cgroup `cpu.weight`, whose mapping depends on the runtime. It neither reserves cores nor increases database pools.
`LOAD_POSTGRES_CPU_SHARES` controls PostgreSQL's relative CPU priority in the isolated load stack (default `0`). Verify the actual cgroup weight; CPU shares are not a core reservation.
`LOAD_WORKER_CPUS` sets the CPU quota per worker replica; default `0` is unlimited.

`LOAD_DIAGNOSTIC_BURST_WORKERS` sets the generator worker ceiling for `flag-burst` (default `64`).
Ordinary flag stages stay at `64`; offered rates and server limits are unchanged.
Check actual delivery and `generator_valid`; a higher worker ceiling does not prove capacity.
Every stage, including overload, requires a valid generator. Burst flag 429s are
allowed, but burst REST traffic must still meet its SLO in the saved-report audit.
`mean_ms` records Vegeta's complete hit duration. `result_consumer_delay_mean_ms`
and `result_consumer_delay_max_ms` separately measure the gap from a finished hit
to receipt by the harness, exposing consumer backpressure and scheduling delay.
Burst pacing permits 50 ms of bounded catchup, half the 100 ms lateness budget:
observed generator wake pauses reached 39 ms and exceeded the earlier 10 ms allowance.
At 400 batches/s this permits up to 20 catchup starts after the delayed original;
it changes burst timing shape and does not prove constant wire arrivals. Ordinary
stages, the 95% delivery / 100 ms lateness gates and actual start-density reporting are unchanged.

Observers reconnect every 20 seconds by default. Set `LOAD_WS_RECONNECT_INTERVAL=0`
for persistent connections. The report records the interval; these are different workloads.

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

For the 28-team final, see [the rehearsal guide](../../deployment/final/README.md).
Its wrapper enables the final capacity profile, monitoring, 224 WebSocket
observers, 40 PUTs per team and PFR public hints. The loopback-only test adapter
models separate client rate buckets; production keeps the real source-IP
limiter. This workload is larger than the historic runs below.
Receiver diagnostics and replica comparisons: [2026-11-02 investigation](results/RECEIVER-CHRP-20261102.txt).
For a CHRP run saved by the isolated comparison wrapper, run
`python3 backend/load_test/audit_report.py <run-directory> --min-rounds 300`.
It checks HTTP, flags, checker rounds and resources. Add
`--require-owned-state --require-pgbouncer --require-windows-host` when those external samplers were started. The auditor
expects the wrapper's `combined/`, `console.log`, `exit-code.txt` and
`completion.json`; it exits nonzero on a failed check.

For physical throughput qualification, add `--require-stable-clocks --require-clock-calibration`.
To require 10,000 successful REST responses/s, also add
`--min-successful-rest-rps 10000`. This checks every steady stage using HTTP 200
counts and external QPC samples bracketing its absolute Linux monotonic start
and response-drain anchors. The samples must share the attack's kernel boot ID;
missing anchors or coverage fail qualification. No clock-rate interpolation is
used, and coarse probe intervals conservatively reduce the measured lower bound.
A nominal 10k stage passing the generator's 95% delivery gate alone does
not establish 10k successful RPS. Increasing the offered rate must be recorded
separately from measured successful throughput.
On Windows, start the read-only sampler before measured traffic:

```powershell
python backend/load_test/scripts/probe_clock.py --distro ForcAD-Load --duration 21600 --completion "<run-directory>/completion.json" --out "<run-directory>/clock-calibration.jsonl"
```

The guards require UTC/monotonic agreement and independent Windows QPC calibration
within 0.1%, including round-trip uncertainty, covering every attack (1 s tolerance).
Linux clocks can drift together, so internal agreement alone is insufficient.
The audit reports clock bounds, coverage and tick range; legacy or incomplete
measurements remain explicit limitations. These checks do not change workload or SLOs.

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

`LOAD_RECEIVER_DATABASE_MAX_CONNS` sets each receiver's pool and the matching PgBouncer client budget.

With `LOAD_DIAGNOSTICS=1`, `pool.log` samples every configured API and receiver.
The default `LOAD_POOL_METRICS_TRANSPORT=exec` uses Docker exec. For a native Linux
engine, opt into `LOAD_POOL_METRICS_TRANSPORT=direct LOAD_MONITORING=1` with
`DOCKER_HOST=unix:///var/run/docker.sock` and `DOCKER_CONTEXT` unset to scrape
the owned containers directly. Replica coverage, the metric whitelist and the
one-second pause after each sampling cycle stay the same; invalid modes or
ownership/address mismatches fail diagnostics.
`LOAD_HTTP_INGRESS_TRANSPORT=direct` separately sends only measured REST/flag
attacks to the owned Nginx private address, preserving the original URL and Host.
It requires the same local Linux Docker socket and no Docker context; the default
is `published`. Bootstrap and WebSocket traffic keep the published route.
`http-ingress.json` records the checked container, private endpoint and matching
loopback publication; attack reports label the transport. This diagnostic bypasses
the published-port forwarding path and cannot qualify its end-to-end capacity.
`LOAD_READ_PACING=recent-window` is a separate REST pacing experiment: after a
stall it drops stale slots outside the existing 10 ms catchup window, reserving
one recent due slot for the preceding already-issued tick when present. This
changes recovery policy; the default `discard-backlog` drops the due backlog.
Flag attacks always keep `discard-backlog`. Each attack records
`generator_pacing_policy`; budgets and validation gates remain unchanged.
Receiver samples include flag submission duration histograms for resolve,
rate, admission, storage and total (`forcad_flag_submission_phase_duration_seconds`).
Use them to locate which stage consumed the request deadline during a timeout.
Diagnostics also stream the owned Nginx container into `http-trace.jsonl`, retaining
only requests taking at least 500 ms, HTTP 499/5xx and normalized errors. The trace
records Vegeta sequence, route, upstream timings/address, API IP mapping and
capture counters; it excludes raw URLs, query strings, headers and error text.
`LOAD_HTTP_PHASES=1` separately enables bounded client timing records in each
attack's `transport_phases` report. It retains slow requests, transport failures
and HTTP 5xx (`http_5xx`); expected 429s are not errors. The wrapper adds tracing
overhead and Go's legacy cancellation path, so leave it unset for qualification.
`LOAD_DIAGNOSTICS=1` alone keeps the native transport and existing timeout/caps.
Retention has independent per-attack limits: 128 slow, 32 HTTP 5xx and 128
transport-error records, with separate retained/dropped counters. The 2026-11-05
`191059` diagnostic used a shared error bucket exhausted by 5xx; its missing
timeout traces cannot identify or exclude client delays.
In the four-receiver trace runs on 2026-11-01, pool logs cover only replicas 1-2;
the separate diagnostic submission traces cover all four receivers.

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

Reports with `generator_metric_version: 2` retain cumulative schedule debt in
`generator_lag_*`, including skipped slots. `generator_valid` requires at least
95% delivery and p99 lateness against recorded start slots of at most 100 ms.
The normal SLO still requires zero HTTP errors and response p99 of at most 1 s.
Historical results are unchanged.

Reports include `generator_valid`, `slo_passed`, latency, errors, and flag
reconciliation. A `stress` run allows REST 429 and 503 responses, so a passing
command alone does not establish capacity. Results: [short runs](results/RESULTS-13900KF.txt),
[five-hour results](results/20261013-112709-soak/RESULTS.txt), and the
[five-hour report](results/20261013-112709-soak/latest-report.json).

# Load testing

Vegeta runs against an isolated local Compose stack through Nginx. The runner
builds the images, creates test data and removes its containers and volumes on
exit. Requires Go, Python 3 and Docker Compose. Run from the repository root:

```sh
make -C backend test-load
LOAD_PROFILE=stress LOAD_DIAGNOSTICS=1 make -C backend test-load
LOAD_PROFILE=load make -C backend test-load
LOAD_PROFILE=soak make -C backend test-load
```

Start with smoke, find the capacity boundary with stress, then run load and soak
on the intended host. The current runner starts both the generator and stack
locally; it has no remote-target option. Run performance tests without `-race`.

## Workload

- 20 teams, three small services (`notes`, `vault`, `queue`) with real storage
  and Python CHECK/PUT/GET processes. Each service uses one container with
  separate team namespaces.
- Two API replicas, two receivers, two workers with eight slots each, one ticker.
  Rounds last 10 seconds; flags live for five rounds.
- Six public REST endpoints: status, scoreboard, teams, tasks, config, attack_data.
- 40 flag batches/s, 100 flags per batch: 70 foreign, 10 own, 20 unknown.
  Flags recur, so 4000 submitted flags/s includes duplicates. The fixture
  produces about six new flags/s when rounds stay on schedule.
- 20 WebSocket spectators, reconnecting every 20 seconds.

## Profiles

| `LOAD_PROFILE` | Measurement duration | REST requests/s |
| --- | --- | --- |
| `smoke` (default) | 30 seconds | 500 |
| `load` | 15 minutes | 500 |
| `stress` | 1 minute per step | 500, 1000, 2500, 5000, 10000 |
| `endpoints` | Same steps for each of six endpoints | Same as stress |
| `spike` | 30 seconds baseline + 30 seconds spike | 500, then 10000 |
| `soak` | 4 hours | 500 |

Each run adds warmup (10s), a flag burst to verify rate limiting, and recovery
(30s). Startup and database reconciliation take additional time.

| Setting | Purpose |
| --- | --- |
| `LOAD_DURATION=15s` | Override measurement duration; per step for stress/endpoints/spike |
| `LOAD_WARMUP`, `LOAD_RECOVERY` | Override warmup/recovery duration |
| `LOAD_DIAGNOSTICS=1` | Collect DB locks/WAL, pool/WS counters and available request logs |
| `LOAD_DATABASE_STORAGE=memory` | PostgreSQL on 1 GiB tmpfs; default is `disk` |
| `LOAD_CONTAINER_LOGGING=local` | Bounded Docker local logs; default inherits deployment settings |
| `LOAD_REPORT_DIR` | Override output directory; default is `backend/load_test/reports/` |

## Reading results

Each run writes `reports/<run>/report.json` and `resources.jsonl` with request
results, flag reconciliation, checker progress and container resource usage.

- Check `generator_valid`: at least 95% of offered RPS delivered and scheduling
  lag p99 <= 100 ms. Otherwise the requested load was not reproduced reliably.
- Check `slo_passed`: all requests succeed and p99 <= 1 second. Stress permits
  REST 429/503 without failing the suite, but records a failed SLO for that step.
- Check `correctness_passed`: ordinary flag requests succeed, acknowledgements
  and scores match the DB, checkers progress, WebSockets and recovery work.
  Only the explicit rate-limit burst permits flag 429 responses.

**A test exit of PASS alone does not confirm capacity.** `rejections` separates
rate limits, admission capacity, deadlines and unclassified errors.

## Comparing hosts

Use the same source, profile, duration, storage, logging and diagnostics settings.
The generator, checkers and services share host resources. Keep background work
comparable; diagnostics also add overhead. Short runs locate bottlenecks; use
disk-backed load and soak runs to validate sustained operation.

On the shared Ryzen 7 8845HS / 32 GB host (2026-10-12), 5000 REST RPS with active
checkers produced about 18% admission 503s at 97-100% CPU usage. Stopping workers
and ticker removed those rejections, but also stopped new flag production and
changed DB writes. CPU alone is therefore not a confirmed cause. DB lock waits
and separate disk `WalSync` stalls remain unresolved; 10000 REST RPS and sustained
production capacity have not passed validation. A tmpfs pass does not validate
durable storage.

When moving the current working tree, include `wahrwelt-kit/`: `backend/go.mod`
temporarily references these local libraries.

# Operations

[Documentation index](../README.md#documentation)

Use `./control.sh --help` to list commands. The launcher builds the Go operator
with its build cache and resolves paths from the repository root, even when
called from another directory. `setup` prepares configuration and private runtime
credentials; `start` builds and starts the Compose project. See [CONFIGURATION.md](./CONFIGURATION.md) for
game fields and credential behavior.

The complete command reference is in [CONTROL.md](./CONTROL.md).
Use `./control.sh pause` and `./control.sh resume` to control the game clock and
flag acceptance without stopping containers.

```bash
cp -n config/config.yml.example config/config.yml
# Edit config/config.yml and add executable checkers under checkers/.
./control.sh setup
./control.sh start
```

## Status and credentials

```bash
./control.sh ps
./control.sh logs --tail 100 receiver
./control.sh status
./control.sh tokens
```

The admin username and password are in the private `config/runtime/admin.env`
file. `tokens` prints every team's token from the
database; give each team only its own token. Editing team tokens in the YAML
does not change an initialized game.

## Flag submissions

Participants submit a JSON list of up to 100 strings to `PUT /flags` and send the
team token in `X-Team-Token`. A `200` response contains one result per submitted
item; it does not mean every flag was accepted. `429` means that team's flag
rate budget is exhausted. `503` means admission capacity is full or a service is
temporarily unavailable. Both responses include `Retry-After`; retry with backoff.
A batch commits atomically before successful results are returned. A network timeout
is ambiguous: retrying cannot award points twice, and an already accepted flag
stays "already stolen" after expiry. An uncommitted attempt is not an acceptance.

Expiry uses the round when the receiver has decoded the complete batch, before
database waits. This timestamp is server-generated; synchronize clocks across
receiver and ticker hosts. Pauses, reset and generation changes still prevent
acceptance. See the
[flag submission API contract](../api/routes/flag-submissions.yml) for the full
request and response schema.

## Round limit and scaling

Change the existing game's round limit in PostgreSQL with:

```bash
./control.sh set-round-limit --rounds 300
```

Use `--rounds 0` to remove the limit. The change preserves game state and does
not resume or reset a session. Editing `game.rounds` in YAML affects initial
game setup only.

The Compose defaults are four API, two receiver and two worker replicas, plus one
ticker. Scale services with Compose, for example:

```bash
./control.sh compose up -d --scale receiver=3 --scale worker=3
```

`runtime-capacity` runs inside the deployed API container and reads its settings.
Pass current replica counts because it does not discover them. Compose shares the configured per-role pool limits with every process. If you
add per-role overrides for workers, pass the worker's `WORKER_CONCURRENCY` and
`FORCAD_CHECKER_MEMORY_LIMIT_MB` values through Compose's `exec -e` option:
`./control.sh compose exec -T -e NAME=VALUE api forcad runtime-capacity ...`.

```bash
./control.sh runtime-capacity \
  --api-replicas 4 --receiver-replicas 3 --worker-replicas 3 --ticker-replicas 1
```

The command reports configured ceilings, not throughput or reserved RAM. Compose
allows 256 HTTP requests in flight per API and 128 per receiver: 1024 API slots
and 256 receiver slots with the default replicas. Override these per-replica
limits with `FORCAD_API_HTTP_MAX_INFLIGHT` and `FORCAD_RECEIVER_HTTP_MAX_INFLIGHT`
before starting or recreating containers. These are concurrency limits, not RPS.
Native processes fall back to `HTTP_MAX_INFLIGHT` (128) when no role override is set.
Workers default to 8 checker actions and a 1024 MiB address-space ceiling per
checker on Linux. Redis enforces 200 flags/s with a burst of 400 per team across
replicas; a batch of 100 costs 100.

All application roles connect through PgBouncer in transaction mode. Each role
has a separate database alias and server budget, so flag traffic cannot consume
the checkers' connection slots. Initialization and operator commands connect
directly to PostgreSQL; migrations need session advisory locks.

| Role | pgx connections per replica | PgBouncer server connections, all replicas |
| --- | --- | --- |
| API | 16 | 32 |
| Receiver | 32 | 64 |
| Worker | 16 | 32 |
| Ticker | 8 | 8 |

Set `FORCAD_<ROLE>_DATABASE_MAX_CONNS` for client pools and
`PGBOUNCER_<ROLE>_CONNECTIONS` for server pools, with uppercase role names.
`DATABASE_MAX_CONNS` is a fallback for every role. Client connections open on
demand. The default nine application replicas total 168 client slots; one
PgBouncer instance permits at most 136 server connections across its four aliases.
Scaling receivers increases client slots while preserving their 64-server budget.
Each alias admits up to 256 clients, with 1024 clients across the pooler.

PgBouncer bounds queue waits to 2s, queries to 6s and transactions to 10s.
Its server connections use 5s statement, 2s lock and 10s idle-transaction timeouts.
Protocol prepared statements are enabled; do not add session-level SET, LISTEN or
session advisory locks to application queries. Native processes use the pooler
when `DATABASE_POOLER_ADDR` is set; otherwise pgx connects directly.

PostgreSQL keeps a global ceiling of 500 (`FORCAD_POSTGRES_MAX_CONNECTIONS`).
Additional pooler instances, direct clients and external worker hosts need a
combined budget below the actual server ceiling. `worker` starts a local pooler
on each worker host. Ports 5432 (direct) and 6432 (pooler) bind to loopback.
Redis allows 10000 clients; its Go pools remain bounded at 32 connections per
process. Nginx uses automatic workers, 8192 connections per worker, reusable
upstream connections and a 65536 file-descriptor ceiling. Upstream sockets also
consume Nginx connection slots.

Watch `forcad_database_acquire_wait_seconds_total`,
`forcad_database_acquire_canceled_total`, `forcad_redis_pool_pending_requests`
and `forcad_redis_pool_timeouts_total` on the private metrics listener. Compare
`forcad_websocket_connections` with `forcad_websocket_connection_limit`; growing
`forcad_websocket_slow_client_drops_total` identifies slow stream consumers.
Optional activity-feed notifications may be skipped when a reader's queue is
full, counted by `forcad_websocket_notification_drops_total`. Scores come from
complete scoreboard snapshots; a missed state update still forces reconnection.
Inspect PgBouncer queues without exposing its administration port:

```bash
./control.sh compose exec -T pgbouncer sh -c \
  'PGPASSWORD="$POSTGRES_PASSWORD" exec psql -h 127.0.0.1 -p 6432 -U "$POSTGRES_USER" -d pgbouncer -c "SHOW POOLS;"'
```

Rising `cl_waiting` and `maxwait` indicate saturation. Measure query latency and
CPU before raising server pools. Run `make -C deployment test-load` on the target
host before choosing competition limits.

Check PostgreSQL `pg_stat_activity` wait events and `pg_blocking_pids(pid)` when
flag requests time out. Long `WalSync` waits mean commits are waiting for durable
WAL storage; their row locks can stall other submissions. More connections do not
shorten that wait. Compare failed-request counts as well as p99 latency, and keep
`fsync` and `synchronous_commit` enabled. See the PostgreSQL
[wait-event reference](https://www.postgresql.org/docs/18/monitoring-stats.html).

Application logs use a bounded queue of 4096 entries so a slow output sink does
not stall requests. Overflow drops entries and increments
`forcad_log_dropped_total`; monitor its rate and the container logging driver.
This queue is for diagnostic logs. Accepted flags and scores still require a
durable database commit.

Compose sets soft memory reservations per container, including storage and
startup services. Containers can use more memory; hard caps default to `0`
(unlimited).

| Containers | Soft reservation | Reservation variable | Optional hard-cap variable |
| --- | --- | --- | --- |
| Receiver | 2 GiB | `FORCAD_RECEIVER_MEMORY_RESERVATION` | `FORCAD_RECEIVER_MEMORY` |
| Worker | 8 GiB | `FORCAD_WORKER_MEMORY_RESERVATION` | `FORCAD_WORKER_MEMORY` |
| All others | 1 GiB | `FORCAD_SERVICE_MEMORY_RESERVATION` | `FORCAD_SERVICE_MEMORY` |

Export these variables before running `./control.sh start`. For example,
`FORCAD_WORKER_MEMORY=12g ./control.sh start` keeps the 8 GiB soft reservation
and adds a 12 GiB hard cap per worker. Set any nonzero cap above its reservation.

A [Docker memory reservation](https://docs.docker.com/engine/containers/resource_constraints/#limit-a-containers-access-to-memory)
does not preallocate RAM or guarantee a minimum. Without hard caps, containers
share available host memory and can exhaust it. The default long-running
replicas total 30 GiB of soft reservations; three workers and five receivers
raise that to 44 GiB. Scaling therefore still requires a host memory budget.
`runtime-capacity` reports checker address-space ceilings, not these Compose
reservations. Application `pids_limit` defaults to 512 (`FORCAD_SERVICE_PIDS`),
or 2048 for workers (`FORCAD_WORKER_PIDS`). The worker limit includes its runtime
threads and checker subprocesses; it does not change `WORKER_CONCURRENCY`.

## Shutdown and updates

Stop services while keeping their containers:

```bash
./control.sh stop --timeout 30
```

`compose down --timeout 30` also removes the stopped containers and project
network. PostgreSQL data is in the local `docker_volumes/postgres/data` bind
mount and remains across a normal `down`.

The initializer runs embedded database migrations on startup, then initializes
the game only if it does not already exist. For an application update, rebuild
and start the services:

```bash
./control.sh build
./control.sh compose up -d
```

Use a fresh PostgreSQL database for the first Go installation. The initial schema
uses UUIDs for entity IDs. Databases from the Python platform or earlier integer-ID
development builds are not upgraded; the initializer rejects incompatible schemas
without resetting their data. Back up PostgreSQL before updates. Released schema
changes require new versioned migrations rather than edits to an applied one.

The initial Goose migration has a `Down` section that removes all application
tables and their data. It is for explicitly discarding a schema, not restoring
an earlier game. The initializer and `migrate` command only apply `Up` migrations.

## Reset

These commands require `--confirm` and stop the Compose project. Use dedicated
PostgreSQL and Redis storage: a normal reset clears game tables and flushes the
configured Redis database, then leaves the project stopped. The YAML and
credential files remain. The next `./control.sh start` initializes a new game from the
YAML.

```bash
./control.sh reset --confirm
```

`--full` also removes the verified local PostgreSQL data directory. It supports
the built-in storage layout on a local Linux Docker daemon and refuses external
or unexpected storage. `clean` performs that full reset and removes generated
credential files and the external Compose override under `config/runtime/`;
run `./control.sh setup` before starting again.

```bash
./control.sh reset --full --confirm
./control.sh clean --confirm
```

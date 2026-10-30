# Operations

[Documentation index](../README.md#documentation)

Use `./control.sh --help` or the [Control CLI guide](CONTROL.md) for commands.
See the [manual setup guide](../README.md#manual-application-setup) for first
start and [Configuration](CONFIGURATION.md) for game fields and credentials.
`start` builds and starts containers; it does not start a game session. Use
`pause` and `resume` to control the game clock and flag acceptance without
stopping containers.

With monitoring enabled, use `./control.sh monitoring` for Compose updates,
scaling and shutdown so the overlay stays active. Plain `start`, `scale` and
`compose` do not include it. See [Monitoring](MONITORING.md#start). For the
network demo, use its [manage commands](../deployment/demo/README.md#manage-the-stack).

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

Control readiness/drain clients and host monitoring checks connect directly to
their configured HTTP(S) endpoint and refuse redirects. They ignore shell proxy
settings; drain URLs must not contain userinfo or fragments. Keep these operator
settings trusted. Private management and team addresses remain supported.

### Administrator HTTP clients

Send JSON to `POST /api/admin/login/` and retain its HttpOnly `session` cookie.
The response also contains `csrf_token`; `GET /api/admin/status/` returns the same
token for that session. Send it in `X-CSRF-Token` on every authenticated admin
POST, PUT, PATCH or DELETE, including logout. Login requires `application/json`
(`415` otherwise); a missing or incorrect mutation token returns `403` before
the handler runs. Existing sessions created before this protection require login
again. The built-in frontend and integration clients handle this automatically.

Keep the CSRF token in memory. Do not put it in URLs, cookies, logs or persistent
browser storage. Login/status responses use `Cache-Control: no-store`. Browser
requests must come from the configured public origin; cross-site and sibling-site
Fetch Metadata are rejected. Direct CLI clients may omit browser origin headers,
but still need both the session cookie and CSRF header. Never automatically replay
an administrative mutation after a network error: its side effects may have committed.
This follows the OWASP
[synchronizer token pattern](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html#synchronizer-token-pattern).

### Database credentials and upgrades

`setup` preserves `storages.db.user/password` as the database owner used only by
PostgreSQL, migrations and maintenance. It adds a separate persisted
`storages.db.runtime.username/password`. Applications and PgBouncer receive only
`config/runtime/postgres_runtime.env`; their shared `config/runtime/game.yml`
contains gameplay configuration without `admin` or `storages` secrets.
The privileged `postgres_environment.env` stays with initializer/database services.

On `init`/`migrate`, the initializer provisions the runtime role with CONNECT,
schema USAGE and SELECT/INSERT/UPDATE/DELETE on application tables. It grants
no ownership, schema creation, TRUNCATE, role administration or migration-ledger
access. Repeated initialization preserves game data and credentials. Managed
roles are bound to one database/schema; unrelated roles, inherited privileges
and unexpected extra object grants cause a failure instead of being silently
changed. External databases need an initializer account authorized to create the
restricted role and grant access. Review existing PUBLIC CREATE grants with the
DBA; initialization refuses to inherit them.

Upgrade during a maintenance window: back up the database and private host
configuration, pause the game, stop the old containers, run `./control.sh setup`,
then `./control.sh start`. Preserve the existing database directory and bootstrap
credentials. Check initialization and application health before resuming. This
refreshes the shared configuration volume and replaces old containers holding
the bootstrap environment; do not keep old workers running alongside the new
credentials. No data reset or bootstrap-password rotation is required. If an
explicit runtime username collides or a restore changes the managed scope, choose
a new runtime username in the private host config and repeat setup/initialization;
do not repurpose another deployment's role.

Additional worker hosts must copy the updated initialized configuration, including
the runtime credential, after provisioning on the main host. `worker` refuses a
legacy configuration missing that credential and never generates a replacement
password for an existing main database. Protect the copied host config; runtime
containers receive only the restricted credentials and sanitized YAML. Before
starting its local PgBouncer and workers, the command runs the config loader once
and waits for success. It stages the shared YAML without starting external
storage services or running the initializer/migrations again.

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

Nginx allows 150 REST API requests per second per source IP, with a burst of 60.
When that budget is exhausted, it returns `429` with `Retry-After: 2` (seconds).
The WebSocket endpoint and `/flags` have separate admission rules; this REST
limit does not apply to them. Configure trusted proxies correctly so the limit
uses the client's IP rather than the proxy's IP.

The Compose defaults are four API, two receiver and two worker replicas, plus one
ticker. Scale services with Compose, for example:

```bash
./control.sh compose up -d --scale receiver=3 --scale worker=3
```

Set `FORCAD_WORKER_REPLICAS=3` before `start` to keep three worker replicas as
the Compose default. Workers consume jobs from one queue; a second worker does
not repeat the same checker action or vote on its result. Increase replicas
when unfinished jobs rise while action slots are busy, then watch worker CPU,
memory, timeouts and per-service result rates in the Checker dashboard. The
local network demo has one worker because its containers share a metrics port.

`runtime-capacity` runs inside the deployed API container and reads its settings.
Pass current replica counts because it does not discover them. Compose shares the configured per-role pool limits with every process. If you
add per-role overrides for workers, pass the worker's `WORKER_CONCURRENCY` and
`FORCAD_CHECKER_MEMORY_LIMIT_MB` values through Compose's `exec -e` option:
`./control.sh compose exec -T -e NAME=VALUE api forcad runtime-capacity ...`.

```bash
./control.sh runtime-capacity \
  --api-replicas 4 --receiver-replicas 3 --worker-replicas 3 --ticker-replicas 1
```

## Resource limits

`runtime-capacity` reports configured ceilings, not throughput or reserved RAM. Compose
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
Each alias's client cap defaults to the larger of 256, its server cap, and
`replicas * pgx_connections + 8` diagnostic connections. Explicit
`PGBOUNCER_<ROLE>_CLIENTS` and `PGBOUNCER_MAX_CLIENTS` (default 1024) must cover
declared demand; invalid budgets prevent PgBouncer startup. Twenty workers with
16 pgx slots allow 328 worker clients while retaining 32 worker server connections
and the same timeouts. This admits declared clients; it is not a throughput
guarantee or an increase in PostgreSQL concurrency.

Keep `FORCAD_API_REPLICAS`, `FORCAD_RECEIVER_REPLICAS`, `FORCAD_WORKER_REPLICAS`
and `FORCAD_TICKER_REPLICAS` aligned with actual Compose replicas (0-1024).
`control start --workers N` and explicit `start --scale ROLE=N` options also
set the matching replica budget for that Compose invocation. `control worker`
does the same for its local pooler. These overrides do not persist: configure
the environment for subsequent starts without replica options. Explicit
PgBouncer caps remain unchanged and must still cover the declared demand.
Scaled `start --no-deps` is refused; use `scale` against the existing pooler.
`control scale` checks deployed pooler budgets and refuses a scale-up that exceeds
them; it does not recreate PgBouncer mid-game. Changing environment variables
does not alter an already running pooler. Plan budget changes and pooler
recreation during maintenance. Raw Docker `--scale` bypasses the control check;
keep its counts and declared budgets synchronized.

PgBouncer bounds queue waits to 2s, queries to 6s and transactions to 10s.
Its server connections use 5s statement, 2s lock and 10s idle-transaction timeouts.
Protocol prepared statements are enabled; do not add session-level SET, LISTEN or
session advisory locks to application queries. Native processes use the pooler
when `DATABASE_POOLER_ADDR` is set; otherwise pgx connects directly.

### Verified database TLS

For an external or untrusted database network, add
`deployment/docker/compose.database-tls.yml` after the base/generated Compose
file. Set `FORCAD_DB_TLS_DIR` to an absolute directory containing `ca.crt`,
`pooler.crt` and `pooler.key`. The CA must trust both server certificates;
the pooler certificate must include DNS name `pgbouncer`, and the PostgreSQL
certificate must match `POSTGRES_HOST`. Enable TLS on the external PostgreSQL
server first. Only PgBouncer mounts its key; application/initializer containers
mount the CA file alone. Restrict the key on the host and make it readable by
pooler UID 10001. Do not store private keys in Git.

The overlay requires client-to-pooler encryption and makes applications verify
the pooler's identity; PgBouncer independently verifies PostgreSQL with
`server_tls_sslmode=verify-full`. The initializer verifies its direct PostgreSQL
connection. Missing TLS files fail configuration, and
`FORCAD_DATABASE_REQUIRE_TLS=1` rejects an explicit insecure `DATABASE_URL`.
Native clients can use `POSTGRES_SSLMODE=verify-full` and `POSTGRES_SSLROOTCERT`,
or equivalent URL parameters. Changing an application SSL setting alone does
not configure PgBouncer's upstream connection.

Use the TLS overlay consistently for every Compose operation, including external
worker hosts. The default local bridge configuration does not enable TLS; SCRAM
authentication alone does not encrypt query traffic. Validate valid-CA connections
and rejection of an untrusted CA/hostname in isolation before rollout.

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

`./control.sh down --timeout 30` also removes the stopped containers and project
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
development builds are not upgraded. The single initial migration includes
extra-CHECK scheduling, public checker logs and announcements. Older development
schemas missing those tables, columns or the event index require a fresh schema;
the initializer rejects them without resetting their data. Compatible databases
retain their existing Goose ledger, including development versions 2-4; the initializer does not
renumber or replay them. Those version numbers remain reserved for future
migrations, which must start above 4. The historical PUT-log cleanup is included
in the consolidated initial migration and is a no-op on a fresh empty table;
it is not rerun for databases that already applied version 1. Current workers
redact successful PUT logs on write, and public HTTP responses also redact old
stored output. Back up PostgreSQL before updates. Released schema changes
require new versioned migrations rather than edits to an applied one.

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

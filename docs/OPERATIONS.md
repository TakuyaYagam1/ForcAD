# Operations

[Documentation index](../README.md#documentation)

Use `./control.sh --help` or the [Control CLI guide](CONTROL.md) for commands.
See the [manual setup guide](../README.md#manual-application-setup) for first
start and [Configuration](CONFIGURATION.md) for game fields and credentials.

Run commands from the repository root.

`start` builds and starts containers; it does not start a game session. Use
`pause` and `resume` to control the game clock and flag acceptance without
stopping containers.

With monitoring enabled, use `./control.sh monitoring` for Compose updates,
scaling and shutdown so the overlay stays active. Plain `start`, `scale` and
`compose` do not include it. See [Monitoring](MONITORING.md#start). For the
network demo, use its [manage commands](../deployment/demo/README.md#manage-the-stack).

## Sizing the ForcAD host

These figures cover the ForcAD VM only, not the Router, VPN or team vulnboxes.
The [infrastructure guide](../infrastracture/README.md) covers the full installation.

| Event size | vCPU | RAM | Basis |
| --- | ---: | ---: | --- |
| About 10 teams | 8–12 | 16 GiB | Starting point for rehearsal; not a measured minimum. |
| 22 targets (21 competing teams and one NPC) | 24 | 32 GiB | Provisioned at the Russian Championship final: three services, 60-second rounds, four workers with eight slots each. A reference for a comparable 20+ team event, not a guarantee for every workload. |

The organizers' hypervisor charts show **64.29% CPU** at 01:30 MSK on
November 19, 2026 and **20.77 GiB used** at 04:00 that day. The **9.58 GiB**
reading is from November 22 at 14:00 MSK. These are separate samples for the entire VM,
not ForcAD process memory, event-wide peaks or proof that 16 GiB would have
covered the same final.

The same team count can require different hardware. More services, more
CHECK/PUT/GET actions or slower checkers leave less time to finish a round.
Rehearse on the proposed VM with the real checkers, round length, expected flag
batches and the monitoring you intend to run. Check unfinished jobs at round
boundaries, available host RAM and swap, and `/flags` latency and errors before
adding workers or reducing RAM. See [per-service budgets](#resource-limits).

## Migrating the checker broker

This checkout uses NATS JetStream. Migrate between games, with the old workers
drained and no unfinished checker jobs, or use a fresh rehearsal state. The new
JetStream volume does not import messages from an old RabbitMQ queue.

In an existing `config/config.yml`, replace `storages.rabbitmq` with
`storages.nats`, retain `user` and `password`, remove `vhost`, and set `host` and
`port` to the NATS endpoint (the supplied Compose service is `nats:4222`). The
strict configuration decoder rejects the old storage section. Run setup again
and rebuild the stack; runtime credentials now live in
`config/runtime/nats_environment.env`. Treat the old broker credential file as
a secret until it is removed; `clean` also removes that legacy generated file.

Ticker provisions the shared persistent stream and durable consumer at startup,
even before a game starts. Workers fetch only when a local slot is free and
acknowledge after SQL settlement. SQL claims and leases remain authoritative
when messages are redelivered. The standard `forcad-nats` volume survives
container recreation and `down` without `--volumes`; `reset` and `clean` remove
it with the other Compose volumes. An external NATS stream is not purged by
these commands. Broker monitoring binds to host loopback port 8222; it is not
an authenticated public endpoint.

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

The admin header separates Teams, Game and Services. Teams and Services list
active and inactive records with links to their editors and create forms. Select
up to 100 active records to disable or inactive records to enable, then confirm
the batch. Changes show a success notification and preserve history and tokens.
Individual records can also be toggled in their editors. Game opens the
scoreboard, lifecycle controls and game settings.
In the create form, the `player` API kind is labeled Team/Команда. Choosing NPC
presets the locked name `NPC`; the organizer enters its IP address.

The public scoreboard fits 20 teams in a normal desktop viewport, including
when a game-status notice appears above the table. Additional teams scroll
inside the table; very short windows may need table scrolling sooner.

Control readiness/drain clients and host monitoring checks connect directly to
their configured HTTP(S) endpoint and refuse redirects. They ignore shell proxy
settings; drain URLs must not contain userinfo or fragments. Keep these operator
settings trusted. Private management and team addresses remain supported.

### Administrator HTTP clients

Send JSON to `POST /api/v1/admin/login/` and retain its HttpOnly `session` cookie.
The response also contains `csrf_token`; `GET /api/v1/admin/status/` returns the same
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

Lifecycle clients call `POST /api/v1/admin/game/{action}/`, where `action` is
`start`, `start_practice`, `start_final`, `pause`, `resume` or `finish`.
`start_final` and `finish` require `{"confirm":true}`. Include the current
`generation` when the client must reject a stale lifecycle change.

The Game settings editor writes to the initialized PostgreSQL game without
restarting services. Before the first game or rehearsal, organizers may change
the mode, round duration, flag lifetime, inflation, Volga attacks mode, scoring
hardness, extra checks, timezone and scheduled start. During a running or paused
game, only the round limit can change; a finished game cannot be edited. A stale
session generation or a concurrent start returns `409` and requires reloading
the form. The authenticated API is `GET`/`PUT /api/v1/admin/game/config/`.
Batch soft-activation is `POST /api/v1/admin/teams/activate/` or
`POST /api/v1/admin/tasks/activate/`; batch soft-deactivation uses the corresponding
`/deactivate/` paths. Send `{"ids":["<uuid>"]}`. All IDs must exist, the list must
contain 1-100 unique IDs, and one transaction either applies the entire change
or none. Already-active IDs are accepted on activation but not counted;
already-inactive IDs are accepted on deactivation but not counted.

The scheduler visits only active teams and services when creating new checker
jobs. A job already queued before deactivation may still finish. Re-enabling a
record in a finished game does not restart rounds or resume checker scheduling.

Game settings and disabled records survive ordinary restarts and `setup` while
the database remains initialized. Explicit `reset` or `clean` clears game data;
the next initialization imports initial values from the host `config.yml` again.

### Competition backups

Open **Game settings -> Backup**, or use the [control CLI](CONTROL.md#examples).
Export is available before the first round, during play and after the final.
The game keeps running during export; database rows come from a single read-only snapshot.
The `.tar.gz` holds a versioned `manifest.json`, one `data/*.ndjson` member per
game table, and local team images in `team-logos/`.

Import previews the event before confirmation. The destination must have the
backup-capable schema and be initialized, with no started game, results or
announcements. Use a future scheduled start to prevent a fresh board from
automatically starting before import. The initial settings, teams and tasks
are replaced in one database transaction with data from the archive.
Import waits up to two seconds for an active ticker or another transaction to
release the game configuration. If it cannot lock the destination, it returns
HTTP 409 without changing the board; retry after the competing operation ends.
The scoreboard remains readable during import.
Imported logos are served from the database so they commit with the event;
existing files in `public/teams/` are not overwritten.

A running game is restored paused at the snapshot time. The original final start
time still applies: a waiting board can start immediately, and an overdue final
replaces a restored practice game. A finished final cannot be resumed.

The `backup_team_logos` table is part of the initial schema (migration `1`).
Goose does not replay it on a database that already applied an older copy of
migration `1`; initialize a separate database for a restore target in that case.

The archive does not contain admin credentials or sessions, checker
executables, vulnbox data, broker queues, or host configuration. It is not a
substitute for a PostgreSQL disaster recovery backup. Keep copies private:
the archive includes team credentials, flags, and the private checker journal.
Archive limits: 256 MiB compressed, 2 GiB unpacked, and 4 MiB per team logo.

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

Admin login has a separate Nginx limit of 1 request/second per client IP with
a burst of 5. Requests over this limit return `429` with `Retry-After: 2`.
Compose disables the backend login limiter only while this ingress limit is
active; direct API deployments still enforce the backend per-IP budget.

Production Compose starts four API replicas, two flag receivers, four checker
workers and one ticker. Each worker admits eight checker actions, for **32 slots**
in all. These are the defaults for the [championship profile](#sizing-the-forcad-host).
For example, to try a fifth worker on a stack without the monitoring overlay:

```bash
./control.sh scale -s worker 5
```

Set `FORCAD_WORKER_REPLICAS=5` before `start` to keep that size across
recreations. With monitoring enabled, [keep its overlay](MONITORING.md#start).
Workers consume from one queue; adding a worker does not repeat a checker
action. Rehearse a scale-up when unfinished jobs carry over into the next
round and `ForcadCheckerSlotsBusy` fires (>90% of slots busy for one minute).
Check CPU, free host memory and checker latency first. A flag-submission
spike alone is not a reason to scale checkers: `/flags` uses the receivers.
The local network demo keeps one worker because its containers share a metrics port.

`runtime-capacity` runs inside the deployed API container and reads its settings.
Pass current replica counts because it does not discover them. Compose shares the configured per-role pool limits with every process. If you
add per-role overrides for workers, pass the worker's `WORKER_CONCURRENCY` and
`FORCAD_CHECKER_MEMORY_LIMIT_MB` values through Compose's `exec -e` option:
`./control.sh compose exec -T -e NAME=VALUE api forcad runtime-capacity ...`.

```bash
./control.sh runtime-capacity \
  --api-replicas 4 --receiver-replicas 2 --worker-replicas 5 --ticker-replicas 1
```

## Resource limits

`runtime-capacity` reports configured ceilings, not throughput or reserved RAM. Compose
allows 256 HTTP requests in flight per API and 128 per receiver: 1024 API slots
and 256 receiver slots with the default replicas. Override these per-replica
limits with `FORCAD_API_HTTP_MAX_INFLIGHT` and `FORCAD_RECEIVER_HTTP_MAX_INFLIGHT`
before starting or recreating containers. These are concurrency limits, not RPS.
Native processes fall back to `HTTP_MAX_INFLIGHT` (128) when no role override is set.
Workers default to 8 checker actions and a 1024 MiB address-space ceiling per
checker on Linux. Redis enforces 200 flags/s with a burst of 500 per team across
replicas; a batch of 100 costs 100.

All application roles connect through PgBouncer in transaction mode. Each role
has a separate database alias and server budget, so flag traffic cannot consume
the checkers' connection slots. Initialization and operator commands connect
directly to PostgreSQL; migrations need session advisory locks.

| Role | pgx connections per replica | PgBouncer server connections, all replicas |
| --- | --- | --- |
| API | 16 | 32 |
| Receiver | 16 | 64 |
| Worker | 4 | 32 |
| Ticker | 8 | 8 |

Set `FORCAD_<ROLE>_DATABASE_MAX_CONNS` for client pools and
`PGBOUNCER_<ROLE>_CONNECTIONS` for server pools, with uppercase role names.
`DATABASE_MAX_CONNS` is a fallback for every role. The 11 default application
replicas allow 120 pgx clients (64 API, 32 receiver, 16 worker, 8 ticker).
PgBouncer permits up to 136 server connections across its four aliases.
The API client and server pools are prewarmed: their minima default to their
configured maxima (16 and 32). Set `FORCAD_API_DATABASE_MIN_CONNS=0` and
`PGBOUNCER_API_MIN_CONNECTIONS=0` to open API connections on demand.
Scaling receivers increases client slots, not their 64-server budget.
Each alias's client cap defaults to the larger of 256, its server cap, and
`replicas * pgx_connections + 8` diagnostic connections. Explicit
`PGBOUNCER_<ROLE>_CLIENTS` and `PGBOUNCER_MAX_CLIENTS` (default 1024) must cover
declared demand; invalid budgets prevent PgBouncer startup. For example, 20
workers explicitly configured with 16 pgx slots each allow 328 worker clients
while retaining 32 worker server connections. This admits declared clients; it
does not guarantee throughput or increase PostgreSQL concurrency.

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

PostgreSQL defaults to 200 connections (`FORCAD_POSTGRES_MAX_CONNECTIONS`),
`shared_buffers=2GB` (`FORCAD_POSTGRES_SHARED_BUFFERS`) and a 512 MiB `/dev/shm`
(`FORCAD_POSTGRES_SHM_SIZE`). Before upgrading an existing deployment from the
old 500-connection default, count all poolers and direct clients, including
external worker hosts. Their combined budget must fit the server.
`worker` starts a local pooler on each worker host. Ports 5432 (direct) and
6432 (pooler) bind to loopback.
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

Compose uses the following **soft reservations per container**. Configure a
role with `FORCAD_<ROLE>_MEMORY_RESERVATION` and an optional hard cap with
`FORCAD_<ROLE>_MEMORY` (uppercase role names). Nginx and the base startup
services use `FORCAD_SERVICE_MEMORY_RESERVATION` / `FORCAD_SERVICE_MEMORY`.

| Containers | Soft reservation per replica |
| --- | ---: |
| Worker | 768 MiB |
| API, receiver, Redis, NATS, Grafana | 512 MiB |
| PostgreSQL | 4 GiB |
| Prometheus | 2 GiB |
| Ticker, PgBouncer, Nginx, base startup services | 128 MiB |

Hard memory caps default to `0` (unlimited). The Go runtime has separate soft
targets: `GOMEMLIMIT` is 512 MiB for API (`GOGC=200`), 768 MiB for receiver
and 192 MiB for worker. The worker target does **not** cover checker subprocesses.
Set overrides in `deployment/docker/.env` before recreating containers. For
example, `FORCAD_WORKER_MEMORY=4g` limits a worker **and all its checkers**
to 4 GiB; size any hard cap from measured peak usage, not its reservation.

A [Docker memory reservation](https://docs.docker.com/engine/containers/resource_constraints/#limit-a-containers-access-to-memory)
does not preallocate RAM or guarantee a minimum. Without hard caps, containers
share available host memory and can exhaust it. Long-running services total
11.375 GiB of soft reservations without monitoring, or 13.875 GiB including
Prometheus and Grafana. The difference from 32 GiB is **not** guaranteed
headroom; use peak host memory, swap and OOM events from representative rounds.
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
extra-CHECK scheduling, public checker logs, announcements and the NPC team
kind/index. Older development schemas missing those tables, columns or indexes
require a fresh schema; the initializer rejects them without resetting their
data. Folding NPC into the initial migration does not upgrade databases that
already applied version 1; back up and migrate those separately before running
the NPC-capable services.

Compatible databases retain their existing Goose ledger, including development
versions 2-4; the initializer does not renumber or replay them. If an existing
version-1 database lacks `checker_jobs_team_round` and `checker_action_logs_team_round`,
create the missing indexes from the [initial schema](../backend/db/migrations/00001_initial_schema.sql)
during maintenance, without changing the ledger.
The historical PUT-log cleanup is a no-op on a fresh empty table;
it is not rerun for databases that already applied version 1. Current workers
redact successful PUT logs on write, and public HTTP responses also redact old
stored output. Back up PostgreSQL before updates. Released schema changes
require new versioned migrations rather than edits to an applied one.

The initial Goose migration has a `Down` section that removes all application
tables and their data. It is for explicitly discarding a schema, not restoring
an earlier game. The initializer and `migrate` command only apply `Up` migrations.

## Reset

These commands require `--confirm`, stop all project containers and leave the
project down. Use dedicated PostgreSQL, Redis and NATS storage. A normal reset
clears the game tables and configured Redis logical database, then removes the
Compose volumes, including the built-in NATS stream and staged configuration
volume. It preserves `config/config.yml`, generated credential files and
checkers on the host. An external NATS stream is not purged. The next
`./control.sh start` recreates runtime volumes and initializes a new game from
the YAML.

```bash
./control.sh reset --confirm
```

`--full` also removes the verified local PostgreSQL data directory. It supports
only the built-in storage layout on Linux with a local Unix-socket Docker daemon
and refuses external, mounted or unexpected storage. `clean` performs that full
reset and removes generated credential files and the managed external-storage
Compose file under `config/runtime/`; run `./control.sh setup` before starting
again.

```bash
./control.sh reset --full --confirm
./control.sh clean --confirm
```

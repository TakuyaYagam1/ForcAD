# Operations

[Documentation index](../README.md#documentation)

Run from repository root; see [Control](CONTROL.md), [Configuration](CONFIGURATION.md),
[API routes](../api/routes/), [Architecture](ARCHITECTURE.md) and [Monitoring](MONITORING.md).

`start` starts containers, not a game; use the admin panel for game lifecycle.
`pause`/`resume` control clock and flag acceptance without stopping containers.

## Sizing the ForcAD host

Sizes are for the ForcAD VM, not the Router, VPN or team vulnboxes. See the
[infrastructure guide](../infrastracture/README.md).

| Event size | vCPU | RAM |
| --- | ---: | ---: |
| About 10 teams | 8-12 | 16 GiB |
| 22 targets (21 teams and one NPC) | 24 | 32 GiB |

The 10-team size is a rehearsal starting point, not a measured minimum. The
championship used three services, 60-second rounds, four workers and eight slots
per worker; 24 vCPU/32 GiB is a reference, not a guarantee.

Final VM readings: 64.29% CPU at 01:30 MSK on November 19, 2026; 20.77 GiB
used at 04:00 that day; 9.58 GiB at 14:00 MSK on November 22, 2026. Whole-VM
readings, not ForcAD process memory or event peaks; they do not show that
16 GiB covered the same final. Rehearse by service count, check volume and speed.

## Checker broker migration

This checkout uses NATS JetStream; it does not import RabbitMQ queues. Drain old
workers and finish queued jobs before migrating, or use fresh rehearsal state.
See [Configuration](CONFIGURATION.md#admin-and-storage-credentials) and
[Architecture](ARCHITECTURE.md) for broker and worker settings.

Older Python/integer-ID schemas and Go databases missing required objects are
incompatible; do not reset blindly. Initial migration `Down` deletes data
([schema](../backend/db/migrations/00001_initial_schema.sql)).

## Admin access

Admin credentials are in private `config/runtime/admin.env`. `tokens` prints all
team tokens; give each team only its own. YAML edits do not update an initialized
game; database state persists across restarts and `setup`.
Only active teams/tasks get jobs; queued jobs may finish after deactivation.
Activation/deactivation batches accept 1-100 IDs atomically. Of the game settings, only the round limit can change during play; none can change after finish.
Authenticated writes need a session cookie and CSRF header. [Editing game settings](../api/routes/admin-game-config.yml) also requires the current `generation`; [lifecycle actions](../api/routes/admin-game-by-action.yml) accept it as an optional guard. After a timeout, check state before repeating a write.

## Competition backups

Export from Game settings or [CLI examples](CONTROL.md#examples). Export works
before the first round, during play and after the final; it reads a consistent
database snapshot without stopping the game.

The private `.tar.gz` contains `manifest.json`, `data/*.ndjson` and local
`team-logos/`, team tokens, flags and the private checker journal. It excludes
admin credentials/sessions, checker executables, vulnbox data, broker queues
and host config; it is not a PostgreSQL disaster-recovery backup. Limits: 256
MiB compressed, 2 GiB unpacked and 4 MiB per logo. Treat archives as secrets.

Restore only to an initialized board with no prior game, results or announcements.
Set its scheduled start in the future so the game does not start before import.
Import replaces initial settings, teams and tasks in one transaction; a busy
configuration lock returns `409` after two seconds without changing the board.
Imported logos are stored with the event, not over `public/teams/`.

A running snapshot restores paused at its snapshot time, but the original final
start still applies; an overdue final may start immediately and replace
practice. A finished final cannot resume. If the target lacks
`backup_team_logos`, use a separate initialized database.

## Database access and upgrades

`storages.db.user/password` are owner credentials for setup, migrations and
maintenance. Setup adds `storages.db.runtime.username/password` for applications
and PgBouncer in private `config/runtime/postgres_runtime.env`. The runtime role
has CONNECT, schema USAGE and table SELECT/INSERT/UPDATE/DELETE only; no
ownership, DDL, TRUNCATE, role administration or migration-ledger access.
Initializer credentials stay in `postgres_environment.env`; shared `game.yml`
excludes admin/storage secrets. See [credential configuration](CONFIGURATION.md#admin-and-storage-credentials).

External DB initialization needs an account allowed to create/grant the role;
it refuses `PUBLIC CREATE` and unrelated, inherited or extra grants. Repeated
initialization preserves data; never reuse another deployment's runtime role.

For upgrades, back up PostgreSQL and host config, pause, stop old containers and
remote workers, then run `setup` and `start`. Preserve the database directory
and owner credentials; verify health before resuming. Do not reset or rotate
credentials, or leave old workers running with new credentials. See the
[worker-host example](CONTROL.md#examples).

## Flag submissions

See the [flag submission contract](../api/routes/flag-submissions.yml) for
request/response fields. Per-item results do not mean every flag was accepted.
`429` may come from Nginx before team quota checks; honor `Retry-After` and back
off. `503` means capacity or service unavailability.

Batches commit atomically. A timeout is ambiguous; retrying cannot award points
twice, but accepted flags remain stolen after expiry. Expiry uses the round
when the receiver decodes a full batch, before DB waits; synchronize receiver
and ticker clocks. Pause, reset or generation changes prevent acceptance.

## Round limit and scaling

`./control.sh set-round-limit --rounds N` changes an initialized game's limit
without reset or resume; `--rounds 0` removes it. YAML applies only at setup.

Production defaults: 4 API, 2 receivers, 4 workers, 1 ticker; eight checker
slots per worker. Scale only when jobs carry over and
`ForcadCheckerSlotsBusy` exceeds 90% for a minute; check CPU, memory and checker
latency first. A `/flags` spike alone is not a reason to add workers. `scale`
refuses excess PgBouncer demand and does not recreate the pooler mid-game; see
[CLI replica guidance](CONTROL.md#compatibility-and-replica-limits).

## Resource limits

API allows 256 in-flight requests per replica and receiver 128. Workers default
to eight concurrent checker actions; each Linux checker has a 1,024 MiB
address-space ceiling. Ingress and team flag budgets are separate; see
[Compose environment](CONFIGURATION.md#compose-environment).

| Role | pgx clients per replica | PgBouncer server cap |
| --- | ---: | ---: |
| API | 16 | 32 |
| Receiver | 16 | 64 |
| Worker | 4 | 32 |
| Ticker | 8 | 8 |

At default replicas these allow 120 pgx clients and 136 pooled server
connections. Initializers and operator commands connect directly to PostgreSQL;
migrations need session advisory locks. Applications use transaction pooling
and must not rely on session `SET`, `LISTEN` or advisory locks. PgBouncer limits
queue waits to 2s, queries to 6s and transactions to 10s.

PostgreSQL defaults to 200 connections, 2 GiB `shared_buffers` and 512 MiB
`/dev/shm`. Budget poolers, direct clients and external workers before changing
these limits. Reservations are soft hints, not allocated RAM or minimums:
services reserve 11.375 GiB without monitoring and 13.875 GiB with Prometheus
and Grafana. This is not guaranteed headroom; check peak RAM, swap and OOMs.

Inspect PgBouncer without exposing its administration port:

```sh
./control.sh compose exec -T pgbouncer sh -c \
  'PGPASSWORD="$POSTGRES_PASSWORD" exec psql -h 127.0.0.1 -p 6432 -U "$POSTGRES_USER" -d pgbouncer -c "SHOW POOLS;"'
```

Rising `cl_waiting` or `maxwait` indicates a pool queue; measure query latency
and CPU before raising limits. For flag timeouts, inspect `pg_stat_activity`
wait events and `pg_blocking_pids(pid)`. Long `WalSync` waits mean commits wait
for durable WAL; more connections do not fix this. Keep `fsync` and
`synchronous_commit` enabled. From the repository root,
`make -C deployment test-load` exercises checker and spectator traffic.

## Verified database TLS

The TLS overlay protects application-to-PgBouncer and PgBouncer-to-PostgreSQL.
Add `deployment/docker/compose.database-tls.yml`; set `FORCAD_DB_TLS_DIR` to an
absolute directory containing `ca.crt`, `pooler.crt` and `pooler.key`. The CA
must trust both server certificates; pooler cert DNS must be `pgbouncer`, and
PostgreSQL's must match `POSTGRES_HOST`.

Applications verify PgBouncer; PgBouncer verifies PostgreSQL with
`server_tls_sslmode=verify-full`; the initializer verifies its direct
connection too. Restrict `pooler.key` and make it readable by pooler UID 10001.
Never commit it. Use the overlay for every Compose operation, including worker
hosts. `FORCAD_DATABASE_REQUIRE_TLS=1` rejects insecure runtime URLs. The
default bridge has no TLS; SCRAM is not encryption. Test valid/rejected CA and hostname.

## Reset

`stop --timeout 30` stops containers. `down --timeout 30` also removes containers and the network.
Both preserve the PostgreSQL bind mount at `docker_volumes/postgres/data`. `down --volumes` removes named volumes, including NATS, but not that bind mount.

`reset` and `clean` require `--confirm`, stop the project and leave it down.
Reset clears PostgreSQL game tables and the configured Redis DB, then removes
Compose volumes, built-in NATS and staged config. It preserves host
`config/config.yml`, generated credentials and checkers; external NATS is not
purged. The next start initializes from YAML.

`reset --full` also removes local PostgreSQL data. It supports built-in storage
on Linux with a local Unix-socket Docker daemon; external, mounted or unexpected
storage is refused. `clean` already performs the full reset and removes
generated credentials and the managed external-storage Compose file. `clean`
does not take `--full`; run `setup` before restarting.

Commands: `./control.sh reset --confirm`,
`./control.sh reset --full --confirm`, `./control.sh clean --confirm`.

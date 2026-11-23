# Control CLI

[Documentation index](../README.md#documentation)

Run `./control.sh COMMAND` from the repository root, or invoke the script by
path from another directory. It builds the Go operator using Go's build cache.
Go and GNU Make are required; deployment commands also need Docker Compose.
`./control.sh --help` lists commands; `./control.sh COMMAND --help` shows
options without connecting to Docker or storage.

## Commands

| Command | Behavior |
| --- | --- |
| `setup` | Validate and back up YAML, fill missing credentials, write private runtime files and configure external storage. Accepts `--redis`, `--database`, `--nats` host or `host:port`. |
| `build` | Build images; accepts Compose build options and service names. |
| `start` / `up` | Build and start containers in the background. `-w N` / `--workers N` sets worker replicas; other arguments go to Compose `up`. |
| `worker` | Prepare external PostgreSQL, Redis and NATS, then build and start workers with local PgBouncer. Requires all three addresses; `--workers N` defaults to 1. |
| `scale` | Set replicas with repeatable `-s SERVICE COUNT` / `--service SERVICE COUNT`; leaves existing containers intact and does not start dependencies. |
| `pause` / `resume` | Pause or resume clock and flag acceptance; containers keep running. In-flight checker actions may finish. Resume does not reset scores or revive a finished game. |
| `validate` / `validate-config` | Validate YAML without changing it or connecting to storage. `--full` also checks admin/storage settings; `--config PATH` selects a file. |
| `tokens` / `print_tokens` / `print-tokens` | Print team UUIDs, names and tokens from PostgreSQL. Give each team only its own token. |
| `backup` | Export competition state and local logos to a private `.tar.gz`, including before the first round. Accepts `--name EVENT` and `--output PATH`. |
| `restore-backup --confirm ARCHIVE.tar.gz` | Restore to an initialized, clean board before a game starts. Replaces seed settings, teams and tasks; never overwrites a played game. |
| `reset --confirm` | Stop the project, clear game tables and configured Redis DB, remove Compose volumes and leave the project down. External NATS is not purged. See [reset details](OPERATIONS.md#reset). |
| `clean --confirm` | Full reset plus removal of generated credentials and external-storage Compose file. Preserve YAML/checkers; run `setup` before starting. External NATS is not purged. |
| `rd` / `compose` | Run Compose arguments with the correct project paths and external-storage configuration. |
| `monitoring` | Run Compose with the Prometheus/Grafana overlay. See [Monitoring](MONITORING.md#start). |
| `network-config` | Export public team addresses and traffic networks from YAML; no credentials. |
| `network-rules --config PATH` | Print nftables counter rules from exported JSON; does not apply them. |
| `network-metrics --config PATH` | Serve router counter metrics; `--listen` defaults to loopback. Requires the dedicated nftables table. |
| `stop` / `down` | Stop containers, or remove them and the project network. Normal `down` preserves PostgreSQL bind data. |
| `restart` | Restart existing containers; does not rebuild images or apply changed configuration. |
| `ps` / `logs` | Show container status or service logs; accepts Compose options such as `--tail N` and `--follow`. |
| `status` | Print current game runtime status as JSON. |
| `set-round-limit --rounds N` | Change the initialized game's limit without reset or resume. `0` removes it. |
| `runtime-capacity` | Report configured ceilings, not throughput. Accepts API, receiver, worker and ticker replica counts. See [resource limits](OPERATIONS.md#resource-limits). |

Game commands use the first running API container and require a started
deployment. `start` starts containers only; start or finish a game in the admin
panel. `start`, `scale` and `compose` omit monitoring; use its
[overlay commands](MONITORING.md#start) when enabled.

## Examples

Run these from the repository root:

```sh
./control.sh setup
./control.sh validate --full
./control.sh build
./control.sh start -w 3
./control.sh scale -s worker 3 -s receiver 5
./control.sh pause
./control.sh resume
./control.sh print_tokens
./control.sh rd logs --tail 100 ticker
```

Transfer an event between installations. Export works after initialization,
before the first round, during play or after the final. On the destination,
initialize the board but restore before a game starts:

```sh
./control.sh backup --name liga-ctf --output forcad-liga-ctf.tar.gz
# On the destination, after initialization and before starting a game:
./control.sh restore-backup --confirm forcad-liga-ctf.tar.gz
```

Archives contain team tokens, flags and private checker data. Transfer and
store them as secrets; see [competition backups](OPERATIONS.md#competition-backups).

For an additional worker host, first copy the initialized
`config/config.yml` and checkers from the main deployment over a trusted
channel. Storage credentials must match; `worker` refuses basic config rather
than generating passwords. Storage endpoints must be reachable from worker
containers, and the main deployment must already be initialized. Run from the
worker installation's repository root:

```sh
./control.sh worker --database db.internal:5432 \
  --redis cache.internal:6379 --nats broker.internal:4222 -w 3
```

## Compatibility and replica limits

The deprecated `--fast` flag is accepted by `build`, `start`, `worker`, `reset`,
`clean` and `compose`; it changes no behavior. Put it before Compose arguments,
as in `./control.sh rd --fast ps`; later arguments pass through unchanged.

`scale` maps `celery` to `worker`, `http-receiver` to `receiver`, and
`client-api`, `admin-api`, `events` to `api`. Raw Compose commands use current
service names; old Celery and Flower processes are not part of this deployment.

Replica counts are 0-1024. Without `--workers`, `start` uses Compose defaults
(four workers, eight checker slots each); `worker` starts one worker on an
additional host. Explicit `start --workers N` or `start --scale ROLE=N` sets
the matching PgBouncer budget for that invocation only, without changing `.env`
or explicit caps. Persist replicas in the environment for later starts.
Scaled `start --no-deps` is refused; use `scale` with the existing pooler.
`start --no-recreate` also checks deployed budgets. `scale` refuses an increase
that exceeds them and does not recreate PgBouncer mid-game. See
[database pool limits](OPERATIONS.md#resource-limits).

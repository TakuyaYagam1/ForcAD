# Control CLI

[Documentation index](../README.md#documentation)

Run `./control.sh COMMAND` from the repository root, or invoke the script by its
path from another directory. It builds the Go operator using Go's build cache.
Go and GNU Make are required; deployment commands also require Docker Compose.
`./control.sh --help` lists commands; `./control.sh COMMAND --help` shows options
without connecting to Docker or storage.

## Commands

| Command | Behavior |
| --- | --- |
| `setup` | Validate YAML, back it up, fill missing credentials, write private runtime files and configure external storage. Accepts `--redis`, `--database`, `--rabbitmq` with a host or `host:port`. |
| `build` | Build container images. Accepts Compose build options and service names. |
| `start` / `up` | Build and start containers in the background. `-w N` / `--workers N` sets worker replicas. Other arguments go to Compose `up`. |
| `worker` | Prepare configuration for external PostgreSQL, Redis and RabbitMQ, then build and start workers with their local PgBouncer. Requires all three storage addresses; accepts `-w N` / `--workers N`, default 1. |
| `scale` | Change replicas with repeatable `-s SERVICE COUNT` / `--service SERVICE COUNT`. Leaves existing containers intact and does not start dependencies. |
| `pause` | Pause the game clock and flag acceptance. Containers keep running; checker actions already in progress may finish. |
| `resume` | Resume the paused game, accounting for elapsed pause time. Does not reset scores or revive a finished game. |
| `validate` / `validate-config` | Validate `config/config.yml` without changing it or connecting to storage. `--full` also requires valid admin and storage settings; `--config PATH` selects another file. |
| `print_tokens` / `print-tokens` / `tokens` | Print team UUIDs, names and tokens from PostgreSQL. Share each team only its own token. |
| `reset --confirm` | Stop the project, clear game data and the configured Redis database. Preserve configuration and checkers; leave the project stopped. `--full` also removes verified local PostgreSQL data. |
| `clean --confirm` | Perform a full reset and remove generated runtime credentials and the external-storage Compose file. Preserve the game YAML and checkers; run `setup` before starting again. |
| `rd` / `compose` | Run arbitrary Compose arguments with the correct project paths and external-storage configuration. |
| `monitoring` | Run Compose with the optional Prometheus/Grafana overlay. See [Monitoring](MONITORING.md). |
| `network-config` | Export public team addresses and traffic networks from the game YAML. No credentials are included. |
| `network-rules --config PATH` | Print nftables counter rules from the exported JSON. Does not apply them. |
| `network-metrics --config PATH` | Serve router counter metrics; `--listen` defaults to loopback. Requires access to the dedicated nftables table. |
| `stop` | Stop containers without removing them. Accepts service names and `--timeout SECONDS`. |
| `down` | Remove containers and the project network. Accepts Compose options; a normal `down` preserves PostgreSQL's bind-mounted data. |
| `restart` | Restart existing containers; it does not rebuild images or apply changed configuration. |
| `ps` | Show container status; accepts Compose `ps` options. |
| `logs` | Show service logs; accepts service names, `--tail N`, `--follow` and other Compose log options. |
| `status` | Print the current game runtime status as JSON. |
| `set-round-limit --rounds N` | Change the initialized game's round limit without resetting or resuming it. `0` removes the limit. |
| `runtime-capacity` | Report configured resource ceilings. Pass `--api-replicas`, `--receiver-replicas`, `--worker-replicas`, `--ticker-replicas`; the command does not discover replicas or measure throughput. |

Game commands run through the first running API container and need a started
deployment. `start` starts containers; use the admin panel to start a practice
or final session and to finish it. See [OPERATIONS.md](./OPERATIONS.md) for reset
storage restrictions, updates and resource budgets.

## Examples

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

To add a worker host, first copy the initialized `config/config.yml` and the
checkers from the main deployment over a trusted channel. The existing storage
credentials must match; `worker` refuses a basic configuration rather than
generating unrelated passwords. The storage endpoints must be reachable from
the worker containers, and the main deployment must already be initialized.

```sh
./control.sh worker --database db.internal:5432 \
  --redis cache.internal:6379 --rabbitmq broker.internal:5672 -w 3
```

## Compatibility

All 12 old CLI commands are available. `--fast` remains an ignored compatibility
flag for `build`, `start`, `worker`, `reset`, `clean` and `rd`/`compose`. For Compose
passthrough, put it before the Compose arguments: `./control.sh rd --fast ps`.
Flags inside a forwarded command are preserved.

`scale` maps `celery` to `worker`, `http-receiver` to `receiver`, and
`client-api`, `admin-api`, `events` to the combined `api` service. Use current
service names for raw Compose commands; the old Celery and Flower processes
are not part of the Go deployment.

Replica counts accept 0 through 1024. Without `--workers`, `start` uses the
Compose defaults (currently two workers), whereas the old CLI defaulted to one.
`reset` and `clean` require explicit `--confirm`; the flag is not added silently.

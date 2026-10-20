# Configuration

[Documentation index](../README.md#documentation)

Copy [config/config.yml.example](../config/config.yml.example) to
`config/config.yml`, edit it, then run `./control.sh setup` from the repository root.
Setup validates the YAML and creates the private runtime environment files.
Unknown fields and invalid values are rejected.

## Game

| Field | Meaning |
| --- | --- |
| `mode` | `classic` runs a full checker workflow each round; `blitz` schedules puts by round and checks/gets by task interval. Default: `classic`. |
| `start_time` | Required scheduled final start. Accepts a local date and time or RFC 3339. |
| `timezone` | Time zone for a start time without an offset. Default: `UTC`. |
| `round_time` | Required round duration in seconds. |
| `extra_checks` | Experimental additional CHECKs per team/task/round, `0..8`. Default: `0`; positive values require `classic`. |
| `rounds` | Positive round limit, or `null`/omitted for no configured limit. |
| `flag_lifetime` | Required flag lifetime in rounds. |
| `default_score` | Default task score. Default: `2500`. |
| `game_hardness` | Finite scoring value greater than 1. Default: `10`. |
| `inflation` | Enables score inflation. Default: `true`. |
| `volga_attacks_mode` | Requires the attacker's matching service to be UP. Default: `false`. |
| `checkers_path` | Base checker directory in the worker. Default: `/checkers/`. |
| `env_path` | Default checker environment path. Default: `/opt/checkers/bin`. |
| `get_period` | Default GET interval in seconds. If omitted, uses `round_time`. |

With `extra_checks > 0`, each additional CHECK runs at a random time in a
separate part of the round. Times are persisted in PostgreSQL and shifted on
resume after a pause. `round_time` must exceed every task's `checker_timeout + 3`.
CHECK must tolerate concurrent invocation alongside the main workflow.
PUT, GET and flag lifetime remain unchanged.

The main workflow and extra CHECKs produce one SLA observation per team/task/round.
Every required job must finish before the result is applied. A confirmed
service failure survives later UP results. An incomplete observation caused by
queue/execution expiry produces CHECK_FAILED without changing the SLA counters,
unless another job confirmed a service failure.

Set this field before initial installation. Editing YAML does not overwrite an
existing game's database settings. It stays disabled by default pending
event-scale validation; the demo explicitly enables two extra CHECKs.

The single initial migration includes all extra-CHECK columns and indexes.
It creates a fresh schema; it does not upgrade an older applied initial schema.
The initializer rejects databases missing required columns without changing data.
Back up an existing game before changing versions. The initial migration's
`Down` removes all application tables and data; it does not just disable CHECKs.
See [operations](OPERATIONS.md#reset) for explicit reset commands.

## Teams

Configure at least one team. `name` and an IPv4 or IPv6 `ip` are required;
`highlighted` defaults to `false`.

`traffic_cidrs` optionally lists the team's service, VPN and farm networks for
monitoring attribution. It defaults to the exact `ip` address. Explicit CIDRs
replace that default, must be canonical and must not overlap another entry.
Validated flag tokens take priority over source IPs. Unlike game settings,
these networks are read from YAML at backend startup; apply edits with `setup`
and restart API/receiver processes. See [Monitoring](MONITORING.md).

`logo_path` is optional. It accepts a safe root-relative path or an HTTP(S) URL.
For a local image, put the file under `public/teams/` and use a path such as
`/team-logos/team-1.svg`. Without a logo, the scoreboard uses its default avatar.

An optional `token` must be a unique 16-character lowercase hexadecimal string.
Omit it or set it to `null` to generate a token during initial database setup.
Configured tokens are imported as written; generated tokens are stored in
PostgreSQL.

## Tasks and checkers

Each task needs a `name` and `checker`. A relative checker path is resolved under
`game.checkers_path`; an absolute path is used as written. Compose mounts the
host's `checkers/` directory read-only in the worker.

| Field | Meaning |
| --- | --- |
| `checker_type` | Checker protocol type. Default: `hackerdom`. |
| `checker_timeout` | Seconds allowed for each checker action. Default: `10`. |
| `puts`, `gets` | Nonnegative action counts. Default: `1` each; their sum cannot exceed `1024`. |
| `places` | Number of flag locations. Default: `1`; must be positive. |
| `env_path` | Task override for `game.env_path`. |
| `default_score` | Task override for `game.default_score`. |
| `get_period` | Task override for the GET interval; otherwise inherits the game value. |

See [CHECKERS.md](./CHECKERS.md) for the executable arguments, output and status
contract.

## Admin and storage credentials

`admin` has `username` and `password`. If the whole section is absent, setup uses
the username `forcad` and generates a password. If the section is present, both
fields are required and the password must be at least 16 characters.

Setup generates storage sections and passwords only when the whole `storages`
section is absent. In that case it uses database `postgres:5432/forcad`,
RabbitMQ `rabbitmq:5672` with virtual host `forcad`, and Redis `redis:6379` with
database `0`. The generated database and RabbitMQ users match the admin username.

If `storages` is present, it must include `db.user`, `db.password`,
`rabbitmq.user`, `rabbitmq.password`, and `redis.password`. Setup does not fill
in missing storage credentials. It defaults only omitted endpoint fields:
database host, port and name to `postgres`, `5432` and `forcad`; RabbitMQ host,
port and virtual host to `rabbitmq`, `5672` and `forcad`; Redis host, port and
database index to `redis`, `6379` and `0`.

The storage sections are `db` (`host`, `port`, `user`, `password`, `dbname`),
`rabbitmq` (`host`, `port`, `user`, `password`, `vhost`) and `redis` (`host`,
`port`, `password`, `db`). Treat `config/config.yml` and `config/runtime/` as
private because they contain credentials.

## Initialization and lifecycle

`forcad init` imports the game, teams and tasks from this file only when the
database has no initialized game. Repeating initialization preserves existing
game data. PostgreSQL is the source of truth after initialization; editing the
YAML does not update an existing game. Use the admin panel for team, task and
lifecycle changes. Use the round-limit command in [OPERATIONS.md](./OPERATIONS.md)
to change an existing game's limit.

The configured start time schedules the final. An administrator can start a
practice session before that time or start a final directly. At the scheduled
final time, practice stops; once no unfinished scheduler jobs remain, practice
scores, flags, history and snapshots are cleared. The `rounds` limit applies to
practice and final sessions.

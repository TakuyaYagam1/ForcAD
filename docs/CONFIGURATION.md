# Configuration

[Documentation index](../README.md#documentation)

Copy [config/config.yml.example](../config/config.yml.example) to
`config/config.yml`, edit it, then run `./control.sh setup` from the repository root.
Setup validates the YAML and creates the private runtime environment files.
Unknown fields and invalid values are rejected.
The parser accepts one YAML document up to 1 MiB. It rejects unknown fields,
YAML aliases, extra documents, invalid scalar types, and values outside the
ranges below.

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
| `get_period` | Default CHECK/GET interval in seconds in `blitz` mode. If omitted, uses `round_time`. |

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

The single initial migration includes all extra-CHECK columns and the NPC team
kind and index. It creates a fresh schema; it does not upgrade an older applied
initial schema. The initializer rejects databases missing required columns or
indexes without changing data.

Back up an existing game before changing versions. The initial migration's
`Down` removes all application tables and data; it does not just disable CHECKs.

See [operations](OPERATIONS.md#reset) for explicit reset commands.

## Teams

Configure at least one player team. `name` and an IPv4 or IPv6 `ip` are required;
`highlighted` defaults to `false`.

An optional `kind` is `player` (default) or `npc`. Configure at most one NPC:

```yaml
teams:
  - name: Team 1
    ip: 10.183.101.2
  - name: NPC test
    ip: 10.183.119.2
    kind: npc
```

Give the NPC its own reachable vulnbox and run the same services on it. The
checker puts fresh NPC flags on every round just as it does for player teams;
players submit them through the usual endpoint. The first accepted submission
per attacker returns the per-flag message `[FLAG] Flag accepted (NPC test). No
points awarded.` (where `FLAG` is the submitted value); repeated submissions
return the ordinary duplicate verdict. NPC captures do not affect
scores, stolen/lost counters, rankings or the public event stream. The public
teams page lists the NPC separately as a test target with its service IP.
Do not distribute the NPC's flag-submission token to participants.

`traffic_cidrs` optionally lists the team's service, VPN and farm networks for
monitoring attribution. It defaults to the exact `ip` address. Explicit CIDRs
replace that default, must be canonical and must not overlap another entry.
Validated flag tokens take priority over source IPs. These networks are read
from the staged YAML at API and receiver startup. After changing them, run
`setup`, then recreate the stack so `config-loader` stages the updated file:

```sh
./control.sh setup
./control.sh compose up -d --force-recreate
```

If the monitoring overlay is active, use:

```sh
./control.sh monitoring up -d --force-recreate
```

Game and task settings still come from PostgreSQL after initialization. See
[Monitoring](MONITORING.md).

`logo_path` is optional. It accepts a safe root-relative path or an HTTP(S) URL.
For a local image, put the file under `public/teams/` and use a path such as
`/team-logos/team-1.svg`. Without a logo, the scoreboard uses its default avatar.

An optional `token` must be a unique 16-character lowercase hexadecimal string.
Omit it or set it to `null` to generate a token during initial database setup.
Configured tokens are imported as written; generated tokens are stored in
PostgreSQL.

## Tasks and checkers

Configure at least one task. Each task needs a `name` and `checker`. To generate
PUT flags, the task name must start with an ASCII letter or digit; that
character becomes the flag prefix. Configuration validation does not check
this prefix, so an invalid name fails when the worker generates a PUT flag. A
relative checker path is resolved under `game.checkers_path`; an absolute path
is used as written. Compose mounts the host's `checkers/` directory read-only
in the worker.

| Field | Meaning |
| --- | --- |
| `checker_type` | `_`-separated output-contract tags. Default: `hackerdom`; `pfr` and `nfr` change successful PUT storage as described in [CHECKERS.md](./CHECKERS.md). |
| `checker_timeout` | Positive seconds allowed for each checker action. Default: `10`. |
| `puts`, `gets` | Nonnegative action counts. Default: `1` each; their sum cannot exceed `1024`. |
| `places` | Number of flag locations. Default: `1`; must be positive. |
| `env_path` | Task override for `game.env_path`. |
| `default_score` | Nonnegative task override for `game.default_score`. |
| `get_period` | Positive task override for the CHECK/GET interval in `blitz` mode; otherwise inherits the game value. |

See [CHECKERS.md](./CHECKERS.md) for the executable arguments, output and status
contract.

## Admin and storage credentials

`admin` has `username` and `password`. If the whole section is absent, setup uses
the username `forcad` and generates a password. If the section is present, both
fields are required, the username must not be empty, and the password must be
at least 16 characters.

Setup generates storage sections and passwords only when the whole `storages`
section is absent. In that case it uses database `postgres:5432/forcad`,
NATS `nats:4222`, and Redis `redis:6379` with database `0`. The generated database
and NATS users match the admin username.

If `storages` is present, it must include `db.user`, `db.password`,
`nats.user`, `nats.password`, and `redis.password`. Setup does not fill
in missing storage credentials. It defaults only omitted endpoint fields:
database host, port and name to `postgres`, `5432` and `forcad`; NATS host and
port to `nats` and `4222`; Redis host, port and
database index to `redis`, `6379` and `0`.

The storage sections are `db` (`host`, `port`, `user`, `password`, `dbname`),
`nats` (`host`, `port`, `user`, `password`) and `redis` (`host`,
`port`, `password`, `db`). Treat `config/config.yml` and `config/runtime/` as
private because they contain credentials.
Custom storage hosts must not be empty, ports must be in `1..65535`, and the
Redis database index must be in `0..1024`. Database user/name and NATS
user/password values must not be empty.

Checker delivery uses NATS JetStream with persistent file storage. Setup writes
`NATS_USER`, `NATS_PASSWORD` and a URL-escaped `NATS_URL` to the private
`config/runtime/nats_environment.env`. All ticker and worker replicas must use
the same `NATS_STREAM` (default `FORCAD_CHECKERS`), `NATS_SUBJECT` (default
`forcad.checker.jobs`) and `NATS_CONSUMER` (default `forcad-workers`).
`NATS_MAX_ACK_PENDING` defaults to 1024 and caps unacknowledged deliveries across
the shared consumer; `WORKER_CONCURRENCY` still caps execution per worker.
The Compose `forcad-nats` volume retains messages across broker restarts.

## Compose environment

`deployment/docker/.env` starts from `frontend/.env.example` and supplies
frontend build-time branding values. It can also hold Compose settings such as
`PUBLIC_ORIGIN` and `GRAFANA_ADMIN_PASSWORD`.

Set `PUBLIC_ORIGIN` to the exact origin users open in their browser, including
the scheme and port when present, for example `https://game.example`. Do not
include a path. ForcAD checks this origin on admin mutations; a mismatch returns
403. If a reverse proxy terminates TLS, set `TRUSTED_PROXY_CIDRS` to that
proxy's source CIDR so ForcAD can recognize HTTPS requests. Keep the proxy list
limited to trusted proxies. `PUBLIC_ORIGIN` may also be exported in the
environment before running Compose.

The default Compose Nginx limits `POST /api/v1/admin/login/` by the client IP
seen at ingress (1 request/second, burst 5). Compose sets
`FORCAD_LOGIN_RATE_LIMIT_AT_PROXY=true` on API replicas to avoid a single
backend bucket shared by every client behind Nginx. Do not enable that setting
when running the API without an ingress login limiter: direct API deployments
keep the application's per-IP limiter by default. Only list actual upstream
proxies in `TRUSTED_PROXY_CIDRS`; never add player VPN ranges to trust
unverified `X-Forwarded-For` values.

Nginx admits `/flags` and its aliases **before** token lookup or database access.
`FORCAD_FLAGS_IP_RPS` (default `400`) and `FORCAD_FLAGS_TOTAL_RPS` (`2000`) are
requests per second per client IP and across this ingress, respectively; each
allows one extra second of burst. A batch of up to 100 flags costs one request.
The authenticated per-team `FLAG_RATE` and `FLAG_BURST` still count individual flags.
`FORCAD_WS_PER_IP` (`128`) limits simultaneous board WebSockets per client IP,
across all API replicas behind this ingress. Excess requests return `429` with
`Retry-After`. Spectators or farms sharing a NAT share an IP budget; size these
settings for that group, or preserve real client IPs through a trusted proxy.
Do not expose API or receiver ports around the ingress.

Backup transfers share four slots. Nginx streams the request to the API without
spooling the archive to disk; the API checks the admin session before decoding
the upload. The 256 MiB archive limit and the admin VPN ACL still apply.

To restrict `/admin`, `/api/v1/admin` and their subpaths, add one or both
settings to `deployment/docker/.env`:

```dotenv
FORCAD_ADMIN_CIDR=10.20.30.0/24
FORCAD_ADMIN_DENY_CIDRS="10.20.30.10 192.0.2.0/24"
```

`FORCAD_ADMIN_CIDR` allows one IP address or network. Leave it empty to use
only the deny list. `FORCAD_ADMIN_DENY_CIDRS` accepts space-separated IPv4 or
IPv6 addresses and CIDRs; it takes precedence over the allowed network.
Blocked requests get `403`, including the admin login. Login and CSRF
requirements still apply to allowed requests. Public routes and `/flags`
are unaffected.

The ACL uses Nginx's TCP peer address and ignores `X-Forwarded-For`. If an
upstream reverse proxy connects to ForcAD on behalf of multiple users, enforce
per-client IP rules at that proxy or connect directly through the admin VPN.
The infrastructure renderer sets `FORCAD_ADMIN_CIDR` from `network.admin_vpn`;
standalone deployments with both settings empty keep the existing behavior.
For an existing deployment, set the variables in `.env` and recreate only
the ingress with `./control.sh compose up -d --no-deps --build nginx`.

Set `GRAFANA_ADMIN_PASSWORD` before starting the monitoring overlay. Grafana
uses it only when initializing an empty Grafana data volume; changing it does
not change an existing account password. See [Monitoring](MONITORING.md#start).

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

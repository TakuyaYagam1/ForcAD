# Configuration

[Documentation index](../README.md#documentation)

Copy [config/config.yml.example](../config/config.yml.example) to
`config/config.yml`, edit it, then run `./control.sh setup` from the repository
root. Setup validates YAML and writes private runtime files. Unknown fields and
invalid values are rejected. The parser accepts one document up to 1 MiB; it
rejects aliases, extra documents, invalid scalar types and out-of-range values.

## Game

| Field | Meaning |
| --- | --- |
| `mode` | `classic` runs the full checker workflow each round; `blitz` schedules PUTs by round and CHECK/GETs by task interval. Default: `classic`. |
| `start_time` | Required scheduled final start; local date/time or RFC 3339. |
| `timezone` | Zone for a start time without offset. Default: `UTC`. |
| `round_time` | Required round duration in seconds. |
| `extra_checks` | Experimental extra CHECKs per team/task/round, `0..8`. Default: `0`; positive values require `classic`. |
| `rounds` | Positive round limit, or `null`/omitted for no limit. |
| `flag_lifetime` | Required flag lifetime in rounds. |
| `default_score` | Default task score. Default: `2500`. |
| `game_hardness` | Finite scoring value greater than 1. Default: `10`. |
| `inflation` | Enables score inflation. Default: `true`. |
| `volga_attacks_mode` | Requires the attacker's matching service to be UP. Default: `false`. |
| `checkers_path` | Worker checker directory. Default: `/checkers/`. |
| `env_path` | Default checker environment path. Default: `/opt/checkers/bin`. |
| `get_period` | Default CHECK/GET interval in `blitz`, seconds; omitted uses `round_time`. |

With `extra_checks > 0`, each additional CHECK runs at a random time in a
separate part of the round. Times persist in PostgreSQL and shift on resume
after a pause. `round_time` must exceed every task's `checker_timeout + 3`;
CHECK must tolerate concurrent calls alongside the main workflow. PUT, GET and
flag lifetime are unchanged.

The main workflow and extra CHECKs produce one SLA observation per
team/task/round. Every required job must finish before results apply. A
confirmed service failure survives later UP results. Queue or execution expiry
produces `CHECK_FAILED` without changing SLA counters unless another job
confirmed a failure.

Set `extra_checks` before initial setup; YAML does not update an initialized game.
The demo sets it to `0`; `2` is a separate experiment, not the demo default.
Migration 1 includes extra-CHECK columns and NPC kind/index but does not upgrade
older schemas. Missing objects are rejected without changing data. Back up
before upgrades: migration `Down` removes all application tables. See
[reset details](OPERATIONS.md#reset).

## Teams

Configure at least one player team. `name` and IPv4 or IPv6 `ip` are required;
`highlighted` defaults to `false`. `kind` defaults to `player`; configure at
most one NPC:

```yaml
teams:
  - name: Team 1
    ip: 10.183.101.2
  - name: NPC test
    ip: 10.183.119.2
    kind: npc
```

Give the NPC a reachable vulnbox running the same services. The checker puts
fresh flags there every round; participants submit them through the usual
endpoint. The first accepted submission per attacker returns
`[FLAG] Flag accepted (NPC test). No points awarded.`; repeats get the normal
duplicate verdict. NPC captures do not affect scores, stolen/lost counters,
rankings or the public event stream. The public teams page lists the NPC
separately with its service IP. Never give participants its submission token.

`traffic_cidrs` optionally lists service, VPN and farm networks for monitoring
attribution. It defaults to the exact `ip`; explicit canonical CIDRs replace
that default and must not overlap. Validated flag tokens take priority over
source IPs. API and receiver read staged YAML at startup. After changes, run
`setup`, then recreate the stack from the repository root:

```sh
./control.sh setup
./control.sh compose up -d --force-recreate
```

With the monitoring overlay active, use:

```sh
./control.sh monitoring up -d --force-recreate
```

Game and task settings come from PostgreSQL after initialization. See
[Monitoring](MONITORING.md).

`logo_path` accepts a safe root-relative path or HTTP(S) URL. For a local image,
put it under `public/teams/` and use a path such as
`/team-logos/team-1.svg`. Without one, the scoreboard uses its default avatar.
An optional `token` must be a unique 16-character lowercase hexadecimal string.
Omit it or set it to `null` to generate a token during initial setup.
Configured tokens are imported as written; generated tokens are stored in
PostgreSQL.

## Tasks and checkers

Configure at least one task. Each needs `name` and `checker`. The first
character of the task name becomes the PUT flag prefix and must be an ASCII
letter or digit; validation does not check it, so an invalid prefix fails when
a worker generates a PUT flag. Relative checker paths resolve under
`game.checkers_path`; absolute paths are used as written. Compose mounts host
`checkers/` read-only in workers.

| Field | Meaning |
| --- | --- |
| `checker_type` | `_`-separated output-contract tags. Default: `hackerdom`; `pfr` and `nfr` change PUT storage. See [CHECKERS.md](./CHECKERS.md). |
| `checker_timeout` | Positive seconds allowed per action. Default: `10`. |
| `puts`, `gets` | Nonnegative action counts. Default: `1` each; sum at most `1024`. |
| `places` | Positive number of flag locations. Default: `1`. |
| `env_path` | Task override for `game.env_path`. |
| `default_score` | Nonnegative override for `game.default_score`. |
| `get_period` | Positive task CHECK/GET interval in `blitz`; otherwise inherits the game value. |

See [CHECKERS.md](./CHECKERS.md) for executable arguments, output and status
contract.

## Admin and storage credentials

`admin` has `username` and `password`. If absent, setup uses username `forcad`
and generates a password. If present, both are required, the username must not
be empty and the password must be at least 16 characters.

Setup generates storage sections and passwords only if all of `storages` is
absent. Defaults are database `postgres:5432/forcad`, NATS `nats:4222` and
Redis `redis:6379`, database `0`; generated database and NATS users match the
admin username. If `storages` is present, it must include
`db.user/password`, `nats.user/password` and `redis.password`; setup does not
fill in missing credentials. Omitted endpoints default to those above.

The storage fields are:

- `db`: `host`, `port`, `user`, `password`, `dbname`, and setup-managed
  `runtime.username/password`.
- `nats`: `host`, `port`, `user`, `password`.
- `redis`: `host`, `port`, `password`, `db`.

Treat `config/config.yml` and `config/runtime/` as private. Storage hosts cannot
be empty; ports are `1..65535`, Redis database is `0..1024`, and database
user/name and NATS user/password cannot be empty.

Setup writes `NATS_USER`, `NATS_PASSWORD` and URL-escaped `NATS_URL` to private
`config/runtime/nats_environment.env`. Ticker and workers must share
`NATS_STREAM` (`FORCAD_CHECKERS`), `NATS_SUBJECT` (`forcad.checker.jobs`) and
`NATS_CONSUMER` (`forcad-workers`). `NATS_MAX_ACK_PENDING` defaults to 1024
across the consumer; `WORKER_CONCURRENCY` still caps each worker. The
`forcad-nats` volume retains messages across broker restarts.

## Compose environment

`deployment/docker/.env` starts from `frontend/.env.example` for build-time
branding and may also hold Compose settings such as `PUBLIC_ORIGIN` and
`GRAFANA_ADMIN_PASSWORD`.

Set `PUBLIC_ORIGIN` to the exact browser origin, including scheme and port but
no path, for example `https://game.example`. A mismatch returns `403`. If a
reverse proxy terminates TLS, set `TRUSTED_PROXY_CIDRS` to its source CIDR;
keep the list limited to trusted proxies. `PUBLIC_ORIGIN` may also be exported
before Compose.

Nginx limits admin login to 1 request/second per client IP with a burst of 5.
Compose sets `FORCAD_LOGIN_RATE_LIMIT_AT_PROXY=true` so backend replicas do not
share one bucket behind ingress. Do not enable it without an ingress limiter;
direct API deployments keep their per-IP limit. Trust only actual upstream
proxies, never player VPN ranges or unverified `X-Forwarded-For`.

Nginx admits `/flags` and aliases before token lookup or database access.
`FORCAD_FLAGS_IP_RPS` defaults to `400` per client IP and
`FORCAD_FLAGS_TOTAL_RPS` to `2000` across ingress; both allow one extra second
of burst. A batch of up to 100 flags costs one request. After authentication,
each team shares a Redis quota across receivers: `FLAG_RATE` defaults to
200 flags/s and `FLAG_BURST` to 500. `FORCAD_WS_PER_IP` defaults to 128
simultaneous board WebSockets per client IP across API replicas. Shared NAT
users share an IP budget; size for the group or preserve client IPs through a
trusted proxy. Do not expose API or receiver ports around ingress.

Backup transfers share four slots. Nginx streams uploads to the API without
spooling archives to disk; the API checks the admin session before decoding.
The 256 MiB archive limit and admin VPN ACL still apply.

To restrict `/admin`, `/api/v1/admin` and their subpaths, set one or both
variables in `deployment/docker/.env`:

```dotenv
FORCAD_ADMIN_CIDR=10.20.30.0/24
FORCAD_ADMIN_DENY_CIDRS="10.20.30.10 192.0.2.0/24"
```

`FORCAD_ADMIN_CIDR` allows one address or network; leave it empty to use only
the deny list. `FORCAD_ADMIN_DENY_CIDRS` accepts space-separated IPv4/IPv6
addresses and CIDRs and takes precedence. Blocked requests, including login,
get `403`; allowed requests still need login and CSRF. Public routes and
`/flags` are unaffected.

The ACL uses Nginx's TCP peer address and ignores `X-Forwarded-For`. If an
upstream proxy represents multiple users, enforce per-client rules there or
connect through the admin VPN. The infrastructure renderer sets
`FORCAD_ADMIN_CIDR` from `network.admin_vpn`; standalone deployments with both
settings empty keep existing behavior. For an existing deployment, set `.env`
and recreate only Nginx from the repository root:

```sh
./control.sh compose up -d --no-deps --build nginx
```

Set `GRAFANA_ADMIN_PASSWORD` before starting the monitoring overlay. Grafana
uses it only when initializing an empty data volume; changing it does not
change an existing password. See [Monitoring](MONITORING.md#start).

## Initialization and lifecycle

`forcad init` imports game, teams and tasks only when the database has no
initialized game. PostgreSQL is the source of truth afterward; YAML edits do
not update an existing game. Use the admin panel for team, task and lifecycle
changes. See [round-limit instructions](OPERATIONS.md#round-limit-and-scaling).

The configured start time schedules the final. An administrator can start
practice earlier or start a final directly. At the scheduled final time,
practice stops; after unfinished scheduler jobs finish, practice scores, flags,
history and snapshots are cleared. `rounds` applies to practice and final.

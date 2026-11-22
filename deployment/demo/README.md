# Local network demo

The Docker Compose demo runs twenty notes services, flag farms, participant and
admin WireGuard, generated router rules, Zeek, ForcAD, Prometheus, and Grafana.
Published ports bind to localhost. Demo services intentionally expose
`/demo/flags` to their farms. The diagram shows two of the twenty team segments.

The sensor runs the router's `zeek-9.0-core` package and generated Zeek policy.
The demo and packet tests build the same [Debian 13 image](../docker/zeek/Dockerfile);
neither needs the upstream plugin manager or development tools.

The board has five service columns. Only `example_service` writes flags. The
other four services return fixed CORRUPT, CHECK FAILED, MUMBLE, and DOWN
statuses so every board color is visible.

```mermaid
flowchart LR
    P1[Player 1 / farm] -->|WireGuard| R[Router WAN]
    P2[Player 2 / farm] -->|WireGuard| R
    A[Admin] -->|Admin WireGuard| R
    R -->|UDP forwarding| VPN[VPN server]
    VPN -->|Original peer IP| R
    R -->|SNAT to 10.183.101.1| V1[Vulnbox 1: 10.183.101.2]
    R -->|SNAT to 10.183.102.1| V2[Vulnbox 2: 10.183.102.2]
    F[ForcAD / checker: 10.183.2.10] --> R
    R -.->|nftables counters| M[Prometheus]
    R -.->|Passive capture before SNAT| Z[Zeek]
    Z -.->|HTTP / WS / TCP / UDP metrics| M
    M --> G[Grafana]
```

## Requirements

Run on Linux with Docker Engine, kernel WireGuard support, and the `wg` tool.
The demo also requires Compose 2.36 or later for `interface_name`, Go, Make,
Python 3.11+, Node.js, and pnpm. See [toolchain versions](../../docs/DEVELOPMENT.md#toolchain).

## Start

Use a separate Docker daemon for the demo if a live game is running. Both
default to the Compose project `forcad`; `demo.sh up` runs `compose down`
before recreating services. A separate checkout or database volume does not
isolate that operation.

Edit [settings.toml](settings.toml) to set teams, the game, and tasks. With an
existing database, keep its team tokens in `config/config.yml` and keep its
game and task settings matched to the TOML. A fresh demo creates that config
and generates missing team tokens. From the repository root, run:

```sh
make -C backend sync-codegen
bash deployment/docker/scripts/demo.sh up
```

`up` prepares the demo configuration, creates missing VPN keys, writes private
runtime files under `config/runtime/demo/`, rebuilds the stack, and recreates
its containers while retaining volumes. The settings use the same schema as
the infrastructure renderer.
The demo PostgreSQL data lives in the `forcad_demo-postgres-data` Docker volume,
separate from the regular deployment's `docker_volumes/postgres/data` bind mount.
`demo.sh down` preserves the demo volume.

The board binds to `127.0.0.1:8080` by default. Set
`FORCAD_DEMO_HTTP_PORT` in `config/runtime/demo.env` when that host port is
occupied, then use the selected port in the board URL.

The demo supports 2 to 32 teams. Append entries to `teams.entries` to add teams
through the admin API and extend the VPN without rotating existing keys.
Existing teams, scores, and tokens stay in place. Reordering or removing teams,
changing an existing entry, or editing the topology requires a separate demo.
Use `demo.sh` for this stack so every Compose overlay remains enabled.

| Service | URL | Credentials |
| --- | --- | --- |
| Board and admin | [localhost:8080](http://localhost:8080/) / [admin login](http://localhost:8080/admin/login) | `config/runtime/admin.env` |
| Grafana dashboards | [ForcAD](http://localhost:3000/d/forcad-monitoring) / [Traffic](http://localhost:3000/d/forcad-traffic) / [DDoS](http://localhost:3000/d/forcad-ddos) / [Relay](http://localhost:3000/d/forcad-relay) | `admin`; password in `config/runtime/demo.env` |
| Prometheus targets | [localhost:9090/targets](http://localhost:9090/targets) | None |

Start practice in the board admin panel. Allow two rounds for checker results
and about one minute for rates. Each farm reads flags from the next team's
`/demo/flags` endpoint, submits them to the board, and generates HTTP,
WebSocket, TCP, and UDP traffic. In **Traffic**, sort active directions by
load and select a team name to inspect a pair; **All teams** clears both
filters. **DDoS** highlights load by victim and source; **Relay** counts
direct vulnbox-to-vulnbox packets dropped by the Router. External paths remain
outside this direct-relay counter; vulnbox Internet access stays enabled.

## Exercise the network

```sh
bash deployment/docker/scripts/demo.sh test
bash deployment/docker/scripts/demo.sh phase pre-game
bash deployment/docker/scripts/demo.sh phase game
bash deployment/docker/scripts/demo.sh load 1 --rps 20 --seconds 45 --ws 5
api/.venv/bin/python deployment/demo/timing.py --rounds 5
```

`test` checks WireGuard routes, SNAT, team isolation, management and metrics
access, pre-game policy, vulnbox filtering, and Prometheus attribution. It
temporarily changes the router phase and adds an nftables test filter, then
removes the filter and restores the saved phase. Farms may time out during
pre-game.

`timing.py` reads completed checker jobs from PostgreSQL and saves their sent,
dispatch-delay, and completion offsets to
`config/runtime/demo/timing.json`. It does not change game state. The offsets
describe whole checker jobs, not individual CHECK, PUT, or GET calls, and do not
test resistance to service-availability manipulation.

## Availability experiment

The demo sets `[game] extra_checks = 0` in `settings.toml` so the board shows
each main CHECK result as soon as it completes. To exercise the availability
experiment, set this value to `2` before creating a fresh demo. In classic
mode, this schedules two additional CHECK-only jobs per team, task, and round.
They run at random times outside the main workflow and do not add flag writes.
All three jobs count toward the round result, so a later UP cannot clear an
earlier failure. The board receives the aggregate result after all three jobs
finish.

Normal installations default to `0`, so extra checks are opt-in. They add
checker load and make the round SLA stricter. The twenty-team demo does not show
that `2` is a suitable general default; test real checkers at the expected
team and service count before enabling them. See
[configuration](../../docs/CONFIGURATION.md#game) for limits and setup.

With a running practice session, 60-second rounds, and `extra_checks = 2`, run:

```sh
api/.venv/bin/python deployment/demo/availability.py healthy --rounds 3
api/.venv/bin/python deployment/demo/availability.py fault --rounds 5
api/.venv/bin/python deployment/demo/availability.py pause
```

`healthy` checks that every team passes. `fault` blocks team 3's notes port for
22 seconds after each 15-second available interval. The fault schedule does
not read checker timing; all other configured teams act as controls. The script
removes its temporary nftables table on exit. `pause` pauses through the admin
API, restarts the ticker and worker, resumes the game, and checks that pending
job IDs and times survive the restart and that the jobs complete. Reports are
written to `config/runtime/demo/availability-*.json`.

These are local fault tests, not production-capacity tests or proof of
continuous uptime. See the [recorded experiment](availability.md) for results
and the default decision.

## Router phase and bounded load

`phase` changes router access independently of the board session. A fresh demo
starts in `game` and saves later phase changes. The host installer defaults to
`pre-game`.

`load N` runs traffic from player N's farm to the next team's vulnbox; the last
team targets team 1. It accepts at most 100 requests per second, 300 seconds,
and 20 WebSockets. It sends health requests only, with no flags or board API
requests. Targets are fixed local fixtures.

| Port | Fixture |
| --- | --- |
| TCP/8080 | Notes, public demo flags, and `/health` with the observed source IP |
| TCP/8081 | WebSocket echo at `/ws` |
| TCP/9001 | TCP echo |
| UDP/9000 | UDP echo |
| TCP/22 | Management ACL probe; no SSH service |

## Manage the stack

```sh
bash deployment/docker/scripts/demo.sh ps
bash deployment/docker/scripts/demo.sh logs -f demo-farm1 demo-farm2 demo-sensor
bash deployment/docker/scripts/demo.sh stop
bash deployment/docker/scripts/demo.sh start
bash deployment/docker/scripts/demo.sh down
```

`down` preserves database, notes, and monitoring volumes. Runtime keys and
credentials live under `config/runtime/`; do not commit them.

The old team pairs `172.30.240.11`/`.12` and `10.101.0.2`/`10.102.0.2` migrate
during `up`. The script saves `config/config_backup_demo_network.yml`, updates
matching database records, retains VPN keys, and saves the prior bundle in
`config/runtime/demo/wireguard-before-network/`. VPN expansion keeps a
`wireguard-before-N-peers/` backup. If a team addition is interrupted, rerun
`up`; its private journal prevents duplicate creation. The demo refuses
unrecognized database teams.
Before replacing old ephemeral notes containers, save their SQLite database
if needed. `config/runtime/demo/seeds/team1/note.db` and
`config/runtime/demo/seeds/team2/note.db` populate empty notes volumes once;
later starts use the persistent volumes.

## Limits

- Generated host rules define router and VPN behavior. Internal Docker bridges
  model VLANs, but the demo does not test 802.1Q tags. Interfaces are named
  `eth3.101` through `eth3.110`. VPN transit uses `/29` to accommodate the
  Docker bridge gateway.
- Nginx, one worker, and Prometheus share the ForcAD namespace. Setup routes
  traffic through the router and redirects TCP/80 to Nginx on port 8080. Worker
  metrics use port 9091. Application containers do not have `NET_ADMIN`.
- Zeek shares the router namespace as UID 10001 with `NET_RAW`. The demo uses
  Zeek's `-C` option for veth checksum offload; the host unit keeps checksum
  validation enabled.
- This tests functionality, not capacity, physical VLAN isolation, Debian boot
  order, or hardware MTU. HTTP metrics cover plaintext HTTP/1. Direct vulnbox
  relays are blocked; indirect external relays remain possible.

See [monitoring](../../docs/MONITORING.md) for metric definitions and limits.

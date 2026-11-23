# Local network demo

Use this demo to check ForcAD's network rules and monitoring on a Linux Docker Engine host. Its 20-team sample runs in an isolated Compose project; published ports bind to loopback.

The demo includes the ForcAD board, router, participant and admin WireGuard clients, sample services, Zeek, Prometheus, and Grafana. Only the dedicated network setup configures routes and firewall rules; application containers do not receive `NET_ADMIN`. Zeek runs as UID 10001 with `NET_RAW`. The notes fixture is for local checks, not a competition service.

## Requirements

Use a Linux host with kernel WireGuard, `wg`, and `iproute2`. The demo also requires Compose 2.36 or later, Go, Make, Python 3.11 or later, Node.js, and pnpm. See [toolchain versions](../../docs/DEVELOPMENT.md#toolchain).

## Start

The demo owns the `forcad-demo` Compose project and keeps generated configuration, keys, and credentials in `.demo/`. Tournament commands use a separate `forcad` project and `config/`. Before recreating containers, `up` checks its bridge subnets against host routes and stops on an overlap with a non-demo route.

Edit [settings.toml](settings.toml) to change the game or team inventory. From the repository root:

```sh
make -C backend sync-codegen
bash deployment/docker/scripts/demo.sh up
```

`up` prepares `.demo/config/config.yml`, creates missing VPN keys, writes private network files under `.demo/network/`, rebuilds the stack, and recreates containers while retaining volumes. The demo supports 2 to 32 teams; the sample has 20. Add teams only by appending to `teams.entries`. Reordering or removing teams, changing an existing entry, or editing the topology requires a separate demo.

The fresh demo starts with router access in `game`, but `up` starts containers only, not a board session. Start a local practice session separately in the admin panel. `bash deployment/docker/scripts/demo.sh phase game` enables game routing; `bash deployment/docker/scripts/demo.sh phase pre-game` restores pre-game routing. These commands change router policy, not session state.

All published ports bind to `127.0.0.1`. Set `FORCAD_DEMO_HTTP_PORT`, `FORCAD_DEMO_GRAFANA_PORT`, or `FORCAD_DEMO_PROMETHEUS_PORT` in `.demo/compose.env` to change their port numbers.

| Role or service | Address | Credential file |
| --- | --- | --- |
| Board administrator | [127.0.0.1:18080](http://127.0.0.1:18080/) and [admin login](http://127.0.0.1:18080/admin/login) | `ADMIN_USERNAME` and `ADMIN_PASSWORD` in `.demo/config/runtime/admin.env` |
| Grafana administrator | [127.0.0.1:13000](http://127.0.0.1:13000/) | User `admin`; `GRAFANA_ADMIN_PASSWORD` in `.demo/compose.env` |
| Prometheus targets | [127.0.0.1:19090/targets](http://127.0.0.1:19090/targets) | None |
| Participant VPN | Per-player config under `.demo/network/players/` | Give each player only its own config |
| Admin VPN | `.demo/network/wireguard/admins/admin-01.conf` | Admin config only |

`.demo/compose.env` also contains `GRAFANA_CHECKER_DB_PASSWORD` for the read-only checker data source; it is not the Grafana login password. These generated credentials are for the local demo, not production. Team tokens are stored under `.demo/network/`. Treat `.demo/` and VPN configs as private; share only each user's role-specific VPN config and do not commit these files.

## Exercise the network

From the repository root:

```sh
bash deployment/docker/scripts/demo.sh test
bash deployment/docker/scripts/demo.sh phase game
bash deployment/docker/scripts/demo.sh load 1 --rps 20 --seconds 45 --ws 5
bash deployment/docker/scripts/demo.sh phase pre-game
api/.venv/bin/python deployment/demo/timing.py --rounds 5
```

`test` checks WireGuard routes, SNAT, team isolation, management and metrics access, pre-game policy, vulnbox filtering, and Prometheus attribution. It temporarily changes the router phase and installs an nftables test filter, then removes the filter and restores the saved phase. Farms may time out during pre-game.

`load` sends health requests to a fixed local fixture only. It does not submit flags or call the board API; limits are 100 requests/s, 300 seconds, and 20 WebSockets. `timing.py` reads completed checker jobs from PostgreSQL and writes offsets to `.demo/network/timing.json`; it does not change game state, measure individual CHECK, PUT, or GET calls, or test resistance to service-availability manipulation.

## Availability experiment

The sample sets `[game] extra_checks = 0`, so the board shows each main CHECK result as soon as it completes. Set `extra_checks = 2` in `settings.toml` before creating a fresh demo to run the availability experiment.

In classic mode, this adds two CHECK-only jobs per team, task, and round at random times outside the main workflow. These extra jobs do not add flag writes. The main workflow and both extra jobs determine the round result; a later UP cannot clear an earlier failure. The board reports the aggregate when all three jobs finish.

Normal installations default to `0`. Extra checks increase checker load and make the round SLA stricter. The 20-team demo does not establish `2` as a suitable general default; test real checkers at the expected team and service count before enabling them. See [configuration](../../docs/CONFIGURATION.md#game).

With a local practice session running, 60-second rounds, and `extra_checks = 2`, run:

```sh
api/.venv/bin/python deployment/demo/availability.py healthy --rounds 3
api/.venv/bin/python deployment/demo/availability.py fault --rounds 5
api/.venv/bin/python deployment/demo/availability.py pause
```

`healthy` checks that every team passes. `fault` blocks team 3's notes port for 22 seconds after each 15-second available interval; the other teams are controls. The script removes its temporary nftables table on exit.

`pause` pauses through the admin API, restarts the ticker and worker, resumes the game, and checks that pending job IDs and times survive and the jobs complete. Reports are written to `.demo/network/availability-*.json`.

These are local fault checks, not capacity tests or proof of continuous uptime. See the [recorded experiment](availability.md) for results and the default decision.

## Manage the stack

```sh
bash deployment/docker/scripts/demo.sh ps
bash deployment/docker/scripts/demo.sh logs -f demo-farm1 demo-farm2 demo-sensor
bash deployment/docker/scripts/demo.sh stop
bash deployment/docker/scripts/demo.sh start
bash deployment/docker/scripts/demo.sh down
```

`down` preserves the PostgreSQL, notes, and monitoring volumes. `up` replaces containers while retaining volumes.

The PostgreSQL volume is `forcad-demo_demo-postgres-data`. Old `forcad_demo-*` volumes and `config/runtime/demo/` are not attached, migrated, or deleted. To retain an old demo's scores, keep its matching configuration and private database backup for an explicit restore.

During `up`, the old team addresses `172.30.240.11`/`.12` and `10.101.0.2`/`10.102.0.2` can be migrated. The script updates matching database records, retains VPN keys, and backs up the old config and VPN bundle under `.demo/`.

If adding a team is interrupted, rerun `up`; its private journal prevents duplicate creation. The demo refuses unrecognized database teams. Back up old ephemeral notes databases before replacing their containers. `.demo/network/seeds/team1/note.db` and `team2/note.db` initialize empty notes volumes once; later starts use persistent volumes.

## Limits

Router rules and VPN behavior come from generated host configuration. Docker bridges model VLANs, but the demo does not test 802.1Q tags, physical VLAN isolation, host boot order, hardware MTU, or capacity. HTTP metrics cover plaintext HTTP/1. Direct vulnbox-to-vulnbox relays are blocked, but external relay paths are outside that counter and vulnboxes retain Internet access. See [monitoring](../../docs/MONITORING.md) for metric definitions and limits.

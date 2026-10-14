# Monitoring

[Documentation index](../README.md#documentation)

## Start

Set `GRAFANA_ADMIN_PASSWORD` in `deployment/docker/.env` or the environment:

```sh
./control.sh monitoring up -d
```

Run the full overlay so API, receiver, worker and ticker expose metrics too.
Containers may be recreated; volumes are preserved. Grafana uses `admin` and the
configured password, independently of the ForcAD login. The setting initializes
new Grafana storage; changing it does not reset an existing account's password.

Keep the overlay when recreating or scaling services:

```sh
./control.sh monitoring up -d --build
./control.sh monitoring up -d --no-recreate --no-deps --scale worker=3 worker
```

Plain `start`, `scale` and `compose up` do not include it and can create replicas
whose metrics are unreachable. For the network demo, always use its `demo.sh` wrapper.

| Endpoint | Default URL |
| --- | --- |
| Grafana | <http://127.0.0.1:3000/> |
| Prometheus targets | <http://127.0.0.1:9090/targets> |
| Prometheus alerts | <http://127.0.0.1:9090/alerts> |

Both bind to localhost. For remote access, forward ports over SSH:

```sh
ssh -L 3000:127.0.0.1:3000 -L 9090:127.0.0.1:9090 USER@FORCAD_HOST
```

Override ports with `FORCAD_GRAFANA_PORT` and `FORCAD_PROMETHEUS_PORT`; an empty
value requests a random host port. Prometheus scrapes replicas through Docker
DNS every 15 seconds and retains up to 15 days or 4 GB. Internal `/metrics`
endpoints have no authentication; keep them private. `/drain` still requires
`ControlToken`.

## Dashboards

| Dashboard | Use |
| --- | --- |
| ForcAD | Per-team board RPS, flags, errors, latency, WS and router traffic |
| Traffic | Selected-team totals for board traffic; router packets/bytes and top source IPs |
| Game traffic | Source team -> target team activity, HTTP/WS, TCP/UDP and capture health |
| Errors | Recent HTTP errors by route, error ratio, latency, WS drops and pool errors |
| Infrastructure | Current pools by component, busiest DB pool, Redis waits and per-replica details |

The main UID is `forcad-monitoring`; others are `forcad-traffic`,
`forcad-game-traffic`, `forcad-errors` and `forcad-infrastructure`.
Links preserve the time range. All sections start expanded. Table heights are
fixed; long tables use pagination. Short graph legends do not use value tables.

**Board metrics:** RPS covers backend requests, including flags, but excludes
static assets and Nginx rejections. WS counts successful upgrades. `Unknown`
means no team attribution. Expand a team row for UUID/IP. Missing sources or
latency samples show N/A. Flag totals use Prometheus `increase`, span game
sessions and are estimates, not authoritative scores. Duplicates are separate
from other rejections.

**Infrastructure and Errors:** select Component, then Replica. Series aggregate
into `api`, `receiver`, `worker` and `ticker`; maximum DB utilization uses the
busiest replica. The Errors Team filter affects HTTP panels only. Pool counts
are application connections, not PostgreSQL sessions or Redis server clients.
For capacity defaults, see [Operations](OPERATIONS.md#resource-limits).

## Router counters

Follow the [ForcAD](../infrastracture/forcad/README.md#monitoring) and
[router](../infrastracture/router/README.md#network-metrics) guides to export the
public inventory, install counters and enable the exporter on private TCP/9109.
No agent is installed on vulnboxes. The VPN must preserve peer IPs; the router
accounts for traffic before game SNAT.

Add only installed targets to `deployment/monitoring/file_sd/routers.yml`.
Leave `[]` when unused. Do not add team labels: each exporter reports all teams.
The router receives public IP/CIDR inventory, never game credentials.

Team metadata refreshes every 15 seconds. Attribution networks come from YAML
`traffic_cidrs` at backend startup: run setup and restart API/receiver after edits.
Router inventory is separate: regenerate JSON/rules together, replace only the
monitoring table and restart the exporter. A fingerprint mismatch fails collection.
See [Configuration](CONFIGURATION.md#teams).

Counter scope:

- Team totals count forwarded IPv4/IPv6 packets matching configured networks,
  including those a later firewall rule may drop.
- Source/vulnbox pairs count packets after router filtering, before SNAT.
  Traffic shows the 50 busiest pairs over five minutes. These count packet
  direction, not who initiated a connection.
- Pair sets expire after 15 minutes and hold 4096 entries per IP family. New
  pairs may be absent when full.
- Local-router, bypassed or offloaded traffic may be invisible. Shared NAT IPs
  cannot separate teams; duplicate vulnbox IPs produce N/A team rates.

Capture each path once. Router counters do not measure HTTP requests or WS.

## Game traffic

Install the [Zeek sensor](../infrastracture/router/README.md#game-httpws-metrics)
and add its private TCP/9911 endpoint to
`deployment/monitoring/file_sd/game-sensors.yml`. Restart existing Prometheus
when adding jobs or rules; target-file updates are discovered automatically.

Zeek passively captures both directions on the router's VPN interface before
filtering and SNAT. Exact participant IPs identify source teams; vulnbox IPs
identify targets. Board, checker and admin traffic are excluded. No proxy or
nginx exporter is required. Unique vulnbox IPs are needed to join team names;
otherwise IP labels remain visible.

Start with **Active directions - last minute**. Sort by RPS, packets/s, bandwidth
or connections. Click a team name in a row to select that direction. Source and
Target filters accept one team or All; **All teams** clears both. Graphs show
selection totals. Inactive directions disappear; unavailable measurements stay
N/A. A team accessing its own box appears on both sides and is not evidence of
an attack.

| Metric | Counts |
| --- | --- |
| HTTP RPS | Parsed plaintext HTTP/1 requests, including upgrades |
| Open WS | Observed HTTP 101 upgrades until TCP close/reset or five idle minutes |
| New TCP flows | Observed flows, including incomplete handshakes |
| Established TCP | Observed established connections |
| New UDP flows | Flows with a 60-second idle timeout, not application requests |

TLS, HTTP/2 and WS message counts are not covered. Captured traffic may be
blocked later. Restarts reset counters and lose existing session state; packet
loss, stream gaps, offloads or asymmetric routing can undercount. No traffic
logs, URLs, headers or bodies are stored.

Failed/stale sensors are omitted from game panels, so multiple sensors can yield
partial totals. Check capture status, heartbeat, drops and gaps before interpreting
traffic. High rates guide investigation; they do not prove a rules violation.

## Alerts and checks

Rules cover DB/Redis pool saturation, HTTP errors, WS capacity/drops, stale
inventory, router collection failures and sensor health. An empty router/sensor
target list has no down alert. Alerts stay in Prometheus: no Alertmanager or
automatic bans are configured.

Not collected: checker backlog/SLA/rounds, PostgreSQL sessions, PgBouncer queues,
Redis server limits and Nginx rejections. Backend disappearance has no dedicated
alert; an empty graph does not establish health.

```sh
make -C deployment test-monitoring
make -C deployment test-network
make -C infrastracture test-game
make -C infrastracture test-dashboards
```

These use isolated containers. Try the [local demo](../deployment/demo/README.md)
for real two-team WireGuard traffic. Router capacity, NIC offloads and Debian
boot order still require staging checks on the intended hosts.

# Monitoring

[Documentation index](../README.md#documentation)

## Start

Set `GRAFANA_ADMIN_PASSWORD` and a separate 32-character-or-longer lowercase
hexadecimal `GRAFANA_CHECKER_DB_PASSWORD` in `deployment/docker/.env` or the
environment:

```sh
./control.sh monitoring up -d
```

Run the full overlay so API, receiver, worker and ticker expose metrics too.
Containers may be recreated; volumes are preserved. Grafana uses `admin` and the
configured password, independently of the ForcAD login. The setting initializes
new Grafana storage; changing it does not reset an existing account's password.
The monitoring overlay also provisions a PostgreSQL reader for the Checker log
tables. It can select only the public checker views, not team tokens or flags.
The local demo generates its own reader password in `.demo/compose.env`.

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

Both services bind to `127.0.0.1` on the ForcAD host. Run a tunnel from your
workstation, replacing the example SSH destination with your ForcAD account and
host:

```sh
ssh -N \
  -L 127.0.0.1:3000:127.0.0.1:3000 \
  -L 127.0.0.1:9090:127.0.0.1:9090 \
  user@forcad-host
```

If SSH access goes through a Router jump host, add `-J user@router-host` before
the final destination:

```sh
ssh -N -J user@router-host \
  -L 127.0.0.1:3000:127.0.0.1:3000 \
  -L 127.0.0.1:9090:127.0.0.1:9090 \
  user@forcad-host
```

Keep `user@forcad-host` as the final SSH target. The forwarded remote
`127.0.0.1` is resolved on ForcAD, where Grafana and Prometheus listen. Then
open the local `http://127.0.0.1` URLs in the workstation browser.

Override ports with `FORCAD_GRAFANA_PORT` and `FORCAD_PROMETHEUS_PORT`; an empty
value requests a random host port. Prometheus scrapes replicas through Docker
DNS every 15 seconds. The default retention is **7 days or 16 GB**, whichever
is reached first. When upgrading from 15 days / 4 GB, set
`FORCAD_PROMETHEUS_RETENTION_TIME=15d` and `FORCAD_PROMETHEUS_RETENTION_SIZE=4GB`
**before** recreating Prometheus if you need the old retention policy. Shortening
retention can delete older samples; raising it again will not restore them.
Queries allow eight concurrent executions and five million samples per query;
adjust `FORCAD_PROMETHEUS_QUERY_CONCURRENCY` and `FORCAD_PROMETHEUS_QUERY_SAMPLES`
only after checking memory pressure. Internal `/metrics` endpoints have no
authentication; keep them private. `/drain` still requires `ControlToken`.

## Dashboards

| Dashboard | Use |
| --- | --- |
| ForcAD | Board overview plus Checker, Errors and Infrastructure sections |
| Traffic | Board totals and game source -> target activity, flows and capture health |
| DDoS | HTTP rate, large observed request bodies and traffic volume by team, WG member, victim and service |
| Relay | Selective failure symptoms for peer → service; confirmed router VLAN drops are shown separately |

The four dashboards are provisioned in English from
`deployment/monitoring/grafana/dashboards/`. Their UIDs are `forcad-monitoring`,
`forcad-traffic`, `forcad-ddos` and `forcad-relay`.

The ForcAD overview is expanded; its other sections can be expanded by
heading. Team selectors support searching.
Table heights remain fixed in native Grafana, but tables scroll inside their
panels instead of using page arrows. The Errors route table and Traffic top
sources table offer 20, 50 or 100 rows through the Rows selector. Short graph
legends do not use value tables.

**Board metrics:** RPS covers backend requests, including flags, but excludes
static assets and Nginx rejections. WS counts successful upgrades. `Unknown`
means no team attribution. Use Expand all / Collapse all above Team overview
to show or hide every team's nested UUID/IP details; individual rows can also
be expanded independently. Missing sources or latency samples show N/A.
Flag totals use Prometheus `increase`, span game sessions and are estimates,
not authoritative scores. Duplicates are separate from other rejections.

**Infrastructure and Errors:** select Component, then Replica. Series aggregate
into `api`, `receiver`, `worker` and `ticker`; maximum DB utilization uses the
busiest replica. The Errors Team filter affects HTTP panels only. Pool counts
are application connections, not PostgreSQL sessions or Redis server clients.
For capacity defaults, see [Operations](OPERATIONS.md#resource-limits).

**Checker:** the first table counts CHECK, PUT and GET actions and result codes
for each configured service in the selected time range. The second table shows
the newest persisted action logs with service, team, round, action, code,
duration and timeout. Service and Team filters narrow both tables. Codes are
101 UP, 102 CORRUPT, 103 MUMBLE, 104 DOWN and 110 CHECK
FAILED. The table does not expose flags or private checker output. The worker
panels below show open jobs, running workflows, occupied action slots, scraped
replicas, container CPU and container memory. CPU and memory include Python
checker subprocesses and require Linux cgroup v2.

Worker metrics count workflows and actions after a confirmed
commit. Retries of an already completed job do not add another sample. A worker
crash or lost commit response can cause an undercount, so these counters are
operational signals rather than authoritative job totals.
Actions from a failed or uncommitted workflow are absent; counters reset when
workers restart. The unfinished-job count includes queued and running jobs and
is collected by ticker from PostgreSQL. A database read failure leaves a gap.
The database reader selects views defined in the initial schema. Checker
messages are withheld because successful PUT messages can contain tokens.
On an existing installation, `checker-db-access` installs or refreshes the
views from that same initial schema before granting access. It stops if the
views are partially present. New services appear in the filter as soon as they
exist in the game database.

## Router counters

Follow the [ForcAD](../infrastracture/forcad/README.md#install-and-test) and
[router](../infrastracture/router/README.md#exporters) guides to export the
public inventory, install counters and enable the exporter on private TCP/9109.
In `infrastracture/settings.toml`, `router_metrics` adds the scrape target; it
does not install the Router exporter. Install that service on the Router with
`sudo bash install.sh --network-metrics`. No exporter runs on vulnboxes.
The VPN must preserve peer IPs; the router accounts for traffic before game
SNAT.

Add only installed targets to `deployment/monitoring/file_sd/routers.yml`.
Leave `[]` when unused. Do not add team labels: each exporter reports all teams.
The router receives public IP/CIDR inventory, never game credentials.

Team metadata refreshes every 15 seconds. Attribution networks come from staged
YAML `traffic_cidrs` at API and receiver startup; see
[Configuration](CONFIGURATION.md#teams) to apply edits. Router inventory is
separate: regenerate JSON/rules together, replace only the monitoring table and
restart the exporter. A fingerprint mismatch fails collection.

Counter scope:

- Team totals count forwarded IPv4/IPv6 packets matching configured networks,
  including those a later firewall rule may drop.
- Source/vulnbox pairs count packets after router filtering, before SNAT.
  Traffic shows the busiest pairs over five minutes. These count packet
  direction, not who initiated a connection.
- Pair sets expire after 15 minutes and hold 4096 entries per IP family. New
  pairs may be absent when full.
- Local-router, bypassed or offloaded traffic may be invisible. Shared NAT IPs
  cannot separate teams; duplicate vulnbox IPs produce N/A team rates.

Capture each path once. Router counters do not measure HTTP requests or WS.

## Game traffic

On an already installed Router use `sudo bash install.sh --monitoring` for the
[Zeek sensor and network exporter](../infrastracture/router/README.md#exporters).
Use the same flag on the managed ForcAD host to update the dashboards, rules and
both scrape targets. For a fresh install, `game_metrics` selects the target; it
does not install Zeek. For a manual setup, add the private TCP/9911 endpoint to
`deployment/monitoring/file_sd/game-sensors.yml`. Reload existing Prometheus
when adding jobs or rules; target-file updates are discovered automatically.

Zeek passively captures both directions on the router's VPN interface before
filtering and SNAT. Exact participant IPs identify source teams; vulnbox IPs
identify targets. Board, checker and admin traffic are excluded. No proxy or
nginx exporter is required. Unique vulnbox IPs are needed to join team names;
otherwise IP labels remain visible.

In **Traffic**, start with **Active directions - last minute**. Sort by RPS,
packets/s, bandwidth or connections. Click a team name in a row to select that
direction. Source and Target filters accept one team or All; **All teams**
clears both. Graphs show selection totals. Inactive directions disappear;
unavailable measurements stay N/A. A team accessing its own box appears on
both sides and is not evidence of an attack. **DDoS** summarizes router rates
by victim and by source; attribution of source peers needs healthy game-sensor
metadata. High rates guide investigation rather than proving a violation.

| Metric | Counts |
| --- | --- |
| HTTP RPS | Parsed plaintext HTTP/1 requests, including upgrades |
| Open WS | Observed HTTP 101 upgrades until TCP close/reset or five idle minutes |
| New TCP flows | Observed flows, including incomplete handshakes |
| Established TCP | Observed established connections |
| New UDP flows | Flows with a 60-second idle timeout, not application requests |

TLS, HTTP/2 and WS message counts are not covered. Captured traffic may be
blocked later. Restarts reset counters and lose existing session state; packet
loss, stream gaps, offloads or asymmetric routing can undercount. The router
does not store bodies, URLs or headers. Large-body evidence is metadata; the
separate optional VPN recorder stores packets.

Failed/stale sensors are omitted from game panels, so multiple sensors can yield
partial totals. Check capture status, heartbeat, drops and gaps before interpreting
traffic. High rates guide investigation; they do not prove a rules violation.

## Selective failures and direct relay attempts

The Router already drops direct configured vulnbox-to-vulnbox forwarding.
Before that drop, its bounded `relay_attempts` nft set counts packets from a
team vulnbox toward another configured vulnbox on the game interfaces. The set
holds at most 4096 distinct configured pairs; once full, new pairs may lack
counters while the firewall still drops their packets. The exporter exposes
`forcad_network_relay_attempt_packets_total{source_team_ip,team_ip}` and
`forcad_network_relay_scrape_success` separately from the existing traffic
counters. The separate router-denied stat sums these packets over the selected
interval. The raw counter gives the two vulnbox IPs, not a participant profile
or an application proxy. Counters reset when router rules are replaced.

Vulnbox WAN egress to public destinations remains allowed for
player-installed monitoring tools. An indirect path through an external
server does not match the direct drop and cannot be reliably detected or
blocked by these Router counters. No automatic bans are configured.

For competition precedent see the FAUST organizers' [traffic fingerprinting
study](https://www.haproxy.com/user-spotlight-series/preventing-traffic-fingerprinting-in-capture-the-flag-competitions).
SNAT conceals source addresses, not all differences between checker and player
requests. This deployment does not insert a transparent application proxy or
rewrite game protocols.

## Judge evidence

1. In **DDoS**, select the UTC interval and target, then compare peers for the
   same service. A complete HTTP/1 request body of at least 1 MiB is not a
   1 MiB IP packet. Zeek counts parsed body bytes, not `Content-Length`;
   supported compression can make this larger than the bytes on the wire.
   TLS, SSH, UDP and HTTP/2 show traffic volume, not request size. Compare
   checker results in the same interval and keep the member's WG `/32`.
2. In **Relay**, start with the cross-profile tables: they rank observed
   failure shares with at least ten attempts (TCP) or ten decoded responses
   (HTTP). Check how other profiles fared on that same target and port;
   fewer than ten comparison observations are marked as insufficient. RST,
   no reply, HTTP 403/429, or checker UP do not prove filtering by regex or IP.
   Check the game phase, allowed port, capture
   health, direction, sample size, and whether other peers exercised an
   equivalent operation. The target normally sees the router's SNAT IP, not
   the original member; a direct VLAN drop never reached that target.
3. If attribution matters, match a legitimate connection while it is still in
   conntrack. On the router, `sudo conntrack -L -p tcp --orig-src "$PEER"
   --orig-dst "$BOX" --sport "$SPORT" --dport "$DPORT"` supplies the original
   and reply tuples (`conntrack` CLI required). With the judge's permission
   compare the translated tuple on the target VLAN, e.g. `sudo timeout 30
   tcpdump -nnqt -i "$TARGET_VLAN" "host $BOX and tcp port $DPORT"`. A VPN-only
   trace cannot show where a missing reply was lost. A completed TCP handshake
   does not establish that the application accepted the request.
4. For endpoint history open a **second** SSH tunnel on a trusted, single-user
   judge workstation. The existing Grafana tunnel remains on ForcAD; this one
   terminates on the VPN:

   ```sh
   ssh -N -o ExitOnForwardFailure=yes \
     -L 127.0.0.1:9837:/run/forcad-wg-history.sock judge@vpn
   ```

   Use the history link in Grafana, or `http://127.0.0.1:9837/`. The VPN
   account needs access to the Unix socket (`root:adm`, mode `0660`). Do not
   bind the forward to a public address. The time is when the authenticated
   peer's endpoint was *observed*, not a login. NAT, mobile failover and
   [WireGuard roaming](https://www.wireguard.com/#built-in-roaming) make IP
   count a poor proxy for headcount. Use the venue's actual main and backup
   egress ranges; an address outside them is a lead, not proof of location.
   Raw endpoints never appear in Prometheus or Grafana labels.
5. On the VPN save **only the incident interval and flow** from the rotating
   capture for the judge file; see [VPN instructions](../infrastracture/vpn/README.md).
   Log UTC, source member, target, protocol, both ports, capture loss, and
   the SHA-256 of the extract. SSH content remains encrypted: the pcap cannot
   identify a person, an AI tool, or commands. Publish the capture policy
   before play, restrict access to judges, and delete case copies after the
   appeal window. No automatic blocking is performed by these observers.

Capture storage is a rate budget, not a team count. Six hours at a combined
100 Mbit/s on `wg-attack` is about 270 GB before capture overhead. A decimal
1 TB disk tops out near 370 Mbit/s over six hours **before** reserved space or
overhead. Five hours contain 60 five-minute intervals; six hours contain 72.
An initial/final partial interval, a restart or the per-file size limit changes
the file count. At high rates the ring retains a shorter history. Check the
oldest available timestamp and free space before promising a replay.
The default active ring has 96 segments, at most 4,000,000 kB each. The
startup check budgets about 405 GB free for that ring, bounded incident
exports and a 10 GiB reserve. An older ring can remain after a restart.
Mount the evidence disk at `/var/lib/forcad-vpn-pcap` before starting the
service; free space on an unmounted disk does not count.

## Alerts and checks

Rules cover DB/Redis pool saturation, HTTP errors, WS capacity/drops, stale
inventory, router network/source collection failures and sensor health. An
empty router/sensor target list has no down alert. Alerts stay in Prometheus:
no Alertmanager or automatic bans are configured.

Not collected: checker per-round SLA, PostgreSQL sessions, PgBouncer queues,
Redis server limits and Nginx rejections. Backend disappearance has no dedicated
alert; an empty graph does not establish health.

```sh
make -C deployment test-monitoring
make -C deployment test-network
make -C infrastracture test-game
make -C infrastracture test-dashboards
```

These use isolated containers. Try the [local demo](../deployment/demo/README.md)
for ten-team WireGuard traffic. Router capacity, NIC offloads and Debian boot
order still require staging checks on the intended hosts.

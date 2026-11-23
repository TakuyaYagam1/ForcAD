# Monitoring

[Documentation index](../README.md#documentation)

## Start

Set `GRAFANA_ADMIN_PASSWORD` and a separate 32-character-or-longer lowercase
hexadecimal `GRAFANA_CHECKER_DB_PASSWORD` in `deployment/docker/.env` or the
environment, then start the full overlay:

```sh
./control.sh monitoring up -d
```

The overlay exposes API, receiver, worker and ticker metrics. Containers can be
recreated without losing volumes. Grafana uses `admin` and its own password;
changing it does not reset an existing account.

Keep the overlay when rebuilding or scaling:

```sh
./control.sh monitoring up -d --build
./control.sh monitoring up -d --no-recreate --no-deps --scale worker=3 worker
```

Plain `start`, `scale` or `compose up` omits the overlay. Grafana and Prometheus
bind to `127.0.0.1` on ForcAD. Forward both ports over SSH; add
`-J user@router-host` after `-N` when using a jump host:

```sh
ssh -N \
  -L 127.0.0.1:3000:127.0.0.1:3000 \
  -L 127.0.0.1:9090:127.0.0.1:9090 \
  user@forcad-host
```

Keep ForcAD as the final SSH target. Open Grafana at
<http://127.0.0.1:3000/>, Prometheus targets at
<http://127.0.0.1:9090/targets> and alerts at
<http://127.0.0.1:9090/alerts>.

Override host ports with `FORCAD_GRAFANA_PORT` and
`FORCAD_PROMETHEUS_PORT`; an empty value requests a random port. Prometheus
scrapes every 15 seconds. Retention is 7 days or 16 GB. Before recreating it,
set `FORCAD_PROMETHEUS_RETENTION_TIME=15d` and
`FORCAD_PROMETHEUS_RETENTION_SIZE=4GB` to keep the old policy. Shortening
retention can delete samples; increasing it later cannot restore them. Queries
allow eight concurrent executions and five million samples each. Unauthenticated
`/metrics` endpoints must stay private; `/drain` requires `ControlToken`.

## Dashboards

The four provisioned dashboards are in English:

- **ForcAD** (`forcad-monitoring`): board, Checker, Errors, Infrastructure.
- **Traffic** (`forcad-traffic`): source-to-target traffic, flows, capture health.
- **DDoS** (`forcad-ddos`): peers above 100 HTTP requests/s over one minute, completed bodies of at least 1 MiB, and traffic by target/service.
- **Relay** (`forcad-relay`): selective failures; Router VLAN drops separately.

RPS includes backend flag requests, excluding static assets and Nginx rejections;
WS counts successful upgrades, and `Unknown` is unattributed. Flag totals are
`increase` estimates, not scores; duplicates are separate from other rejections.
Missing values show N/A. Infrastructure series aggregate API, receiver, worker
and ticker; maximum DB use is the busiest replica. Application pool counts are
not PostgreSQL sessions or Redis clients.

Checker tables show action counts and persisted fields, never flags or private
output. The reader sees only public views; team tokens and flags are excluded,
and successful PUT messages are withheld because they can contain tokens.
Worker counters increment after confirmed commits, may undercount crashes or
lost responses, and reset on restart. Ticker read failures leave gaps. Container
CPU/memory include checker subprocesses and require Linux cgroup v2. See
[Operations](OPERATIONS.md#resource-limits).

## Router counters

`router_metrics` adds a scrape target; it does not install the Router exporter.
Install with `sudo bash install.sh --network-metrics` on the Router (private
TCP/9109). No exporter runs on vulnboxes. Add only installed targets to
`deployment/monitoring/file_sd/routers.yml`; leave `[]` when unused and do not
add team labels. VPN must preserve peer IPs. Router inventory contains public
IP/CIDR data, not game credentials.

Team totals include forwarded IPv4/IPv6 packets that a later rule may drop.
Source/vulnbox pairs count packets after filtering and before SNAT; they show
direction, not the initiator. Pair sets expire after 15 minutes and hold 4096
entries per IP family. Offloaded/bypassed traffic may be invisible; shared NAT
cannot separate teams, and duplicate vulnbox IPs produce N/A rates. Team
attribution uses `traffic_cidrs`; Router inventory and rules are regenerated
together.

The Router drops direct configured vulnbox relays. Its bounded `relay_attempts`
set may omit new counters when full while still dropping packets. Separate
metrics expose `forcad_network_relay_attempt_packets_total{source_team_ip,team_ip}`
and `forcad_network_relay_scrape_success`; replacing rules resets them. They
identify vulnbox IPs, not participants. Indirect relays via public hosts cannot
be reliably detected. There is no transparent proxy or automatic ban.

## Game traffic

On an installed Router, `sudo bash install.sh --monitoring` enables or refreshes
Zeek and the network exporter. The ForcAD add-on updates dashboards, rules and
targets. Neither changes game phase or restarts game containers; the Router
add-on manages its monitoring table and exporters. For a fresh install,
`game_metrics` selects a target but does not install Zeek.

Zeek passively captures both directions on the VPN interface before filtering
and SNAT. Participant IPs identify sources and vulnbox IPs targets; board,
checker and admin traffic are excluded. Unique vulnbox IPs are needed to join
team names. The sensor does not block traffic.

| Metric | Meaning |
| --- | --- |
| HTTP RPS | Parsed plaintext HTTP/1 requests, including upgrades. |
| Open WS | HTTP 101 upgrades until close/reset or five idle minutes. |
| New TCP flows | Observed flows, including incomplete handshakes. |
| Established TCP | Observed established connections. |
| New UDP flows | 60-second idle-timeout flows, not requests. |

`forcad_game_observations_total` records exclusive initial TCP outcomes
(`tcp_established`, `tcp_no_reply`, `tcp_refused`), then later `tcp_reset`.
Plaintext final responses are `http_403`, `http_429` or `http_other`. No
`http_no_reply` is emitted because capture loss and an incomplete observation
cannot be distinguished.

Prometheus metrics are aggregates, not packets or request logs.
`forcad_game_peer_http_requests_total` counts parsed HTTP/1 requests.
`FORCAD_LARGE_HTTP_BODY` is a separate system-journal event, rate-limited to
100 per minute. It records time, peer/target IP and port, protocol and size,
not URLs, headers or bodies; inspect it with
`journalctl -u game-metrics.service`. Large-body metrics count completed
originator bodies of at least 1,048,576 parsed bytes after framing/chunk
decoding. Supported gzip/deflate uses uncompressed length; `Content-Length`
is not used. Interrupted or gapped bodies are excluded;
`Forcad::large_body_threshold` changes the threshold.

TLS, HTTP/2 and WS message counts are not covered. Restarts reset counters and
session state; loss, gaps, offloads, asymmetric routing and stale sensors can
undercount. Captured traffic may be blocked later. A high rate alone is not evidence of a rule violation.

## Judge evidence

- **DDoS:** compare peers on the same service and interval. A 1 MiB parsed
  HTTP/1 body is not a 1 MiB packet. TLS, SSH, UDP and HTTP/2 show volume, not
  request size. Compare checker results and keep the member's WireGuard `/32`.
- **Relay:** comparisons need ten TCP attempts or ten decoded HTTP responses.
  RST, no reply, HTTP 403/429 or checker UP do not prove filtering. Check phase,
  port, capture health, direction, sample size and equivalent peer operations.
  Targets normally see Router SNAT; a direct VLAN drop never reached them.
- **Attribution:** inspect conntrack while a legitimate flow exists:

  ```sh
  sudo conntrack -L -p tcp --orig-src "$PEER" --orig-dst "$BOX" \
    --sport "$SPORT" --dport "$DPORT"
  ```

  With judge permission, compare the translated tuple on the target VLAN:

  ```sh
  sudo timeout 30 tcpdump -nnqt -i "$TARGET_VLAN" \
    "host $BOX and tcp port $DPORT"
  ```

  A VPN-only trace cannot locate a missing reply; a completed handshake does
  not prove application acceptance.
- **VPN evidence:** an endpoint IP is an observed endpoint, not a login or
  location; NAT and mobile failover make IP a poor headcount proxy. Raw
  endpoints never appear in Prometheus/Grafana. Pcap reveals no identity,
  tools or commands inside encrypted SSH. Export only the incident flow,
  record UTC, peer, target, ports, drops and SHA-256; publish policy beforehand,
  restrict access to judges and delete case copies after appeals. See
  [VPN commands](../infrastracture/COMMANDS.md#vpn) for private tunnels and
  capture/export limits.

Observers do not block traffic. Alerts stay in Prometheus; there is no
Alertmanager or automatic ban. Empty targets have no down alert. Backend
disappearance, checker SLA, PostgreSQL sessions, PgBouncer queues, Redis server
limits and Nginx rejections have no dedicated alert. An empty graph does not
establish health.

Run `make -C deployment test-monitoring`, `make -C deployment test-network`,
`make -C infrastracture test-game` and `make -C infrastracture test-dashboards`.
Router capacity, NIC offloads and Debian boot order still require staging.

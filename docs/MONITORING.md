# Monitoring

Set `GRAFANA_ADMIN_PASSWORD`, then start the full stack with `./control.sh monitoring up -d`. The full `up` applies the overlay's metrics listener bind to running API and receiver containers; it preserves existing Compose volumes. Grafana uses the `admin` account and does not reuse ForcAD credentials. The dashboard UID is `forcad-monitoring`. Grafana and Prometheus bind to `127.0.0.1:3000` and `127.0.0.1:9090`; set `FORCAD_GRAFANA_PORT` or `FORCAD_PROMETHEUS_PORT` to an empty value to request a random host port.

Prometheus discovers every API, receiver, worker, and ticker replica through Docker DNS A records on port 9090. Metrics are served at `/metrics` on the internal Compose network without authentication. The separate `/drain` endpoint still uses `ControlToken`. No Docker socket or mandatory exporter is used. Prometheus retains at most 15 days or 4 GB of data.

The dashboard is in Grafana's root. Its table and team legends display names; expand a team row to see its UUID and configured vulnbox IP. Unattributed traffic, including localhost spectators, appears in overall charts as `Unknown`, not as a team row. Its eight metric columns are RPS, flags/s, HTTP 4xx/5xx errors/s, p99 REST API latency, WebSocket connections, outgoing Mbps, incoming Mbps, and packets/s. RPS includes REST endpoints such as flags, status, and attack data; it excludes static assets and requests rejected by Nginx. Application counters sum across API and receiver replicas. If those source roles are unavailable, their values are N/A rather than zeros. Missing p99 samples and router data are also shown as N/A.

Four dashboards live in the root, with links that preserve the selected time range:

| Dashboard | Contents |
| --- | --- |
| ForcAD | Team table, accepted/rejected/duplicate flags for the period, submission outcomes and accepted flags/s by team |
| Traffic | RPS, WS connections and new connections/s by team, WS limits, router Mbps and packets/s, top source IPs to vulnboxes |
| Errors | HTTP codes/routes, error ratio by team, p99, WS event drops, database cancellations and Redis timeouts |
| Infrastructure | Database/Redis client connections, pool utilization, pending requests and acquisition waits |

The main dashboard keeps UID `forcad-monitoring`; the others use `forcad-traffic`, `forcad-errors` and `forcad-infrastructure`. Flag totals use Prometheus `increase`: estimates for the selected period across game sessions, not authoritative game scores. Rejected totals exclude duplicates, shown separately. Rounds and checker SLA are not currently exported as metrics.

Database counts describe application pools connected to PgBouncer, not PostgreSQL server sessions. Redis counts describe application pools, not server `maxclients` or dedicated Pub/Sub connections. Traffic shows successful WS upgrades; it does not identify every attack or include requests rejected by Nginx. Go runtime metrics remain available in Prometheus for profiling.

Default database limits belong to three separate layers:

| Layer | Connections |
| --- | --- |
| Application pools | 168 total: API 4 x 16, receiver 2 x 32, worker 2 x 16, ticker 8 |
| PgBouncer server pools | 136 total: API 32, receiver 64, worker 32, ticker 8 |
| PostgreSQL | `max_connections=500`, configurable with `FORCAD_POSTGRES_MAX_CONNECTIONS` |

The `Application DB connections` panel shows the first layer, summed over healthy replicas. Raising PostgreSQL's limit does not enlarge the other pools. Server connections, PgBouncer queue waits, Redis server limits, Nginx rejections and checker backlog are not currently collected. WS slots are limited per API process, without a per-team quota. Application disappearance has no dedicated alert yet; do not treat an empty chart as proof that the service is healthy.

Router monitoring needs one exporter on the organizer's Linux router, with `nft` and permission to read counters. Nothing is installed on the teams' vulnboxes. On the ForcAD host, export only the public network inventory:

```sh
./control.sh network-config > config/runtime/network.json
./control.sh network-rules --config config/runtime/network.json > config/runtime/network.nft
```

Copy the `forcad` binary, `network.json` and `network.nft` to the router. Review the generated rules, validate with `sudo nft -c -f network.nft`, then apply with `sudo nft -f network.nft`. Only the new `inet forcad_monitoring` table is created. Repeated application fails instead of duplicating counters. Existing filtering rules remain in effect. Run the exporter as a supervised process:

```sh
sudo ./forcad network-metrics --config network.json --listen ROUTER_PRIVATE_IP:9109
```

Allow only Prometheus to reach that private listener. Add `- targets: ["router.private:9109"]` to `deployment/monitoring/file_sd/routers.yml`; leave the file as `[]` until deployed. Do not add team labels to targets: each exporter reports all teams. Never copy the full game config or database credentials to the router.

Backend team metadata refreshes every 15 seconds. Configure VPN/farm networks through team `traffic_cidrs` in [CONFIGURATION.md](CONFIGURATION.md). Changes require restaging the config and restarting API/receiver. Router inventory does not auto-refresh: regenerate it, replace only the monitoring table with the reviewed rules, and restart the exporter. A fingerprint mismatch reports a scrape failure instead of attributing old counters to new teams.

Network rates count forwarded IPv4/IPv6 traffic matching these networks. They include packets reaching the monitoring hook, even if a later firewall rule drops them. The source-IP counters run after router filtering and before postrouting SNAT. They identify the original source IP and destination vulnbox IP for forwarded packets to configured team IPs. The Traffic dashboard shows the 50 busiest source/vulnbox pairs by packets/s over five minutes. The two dynamic nftables sets expire entries after 15 minutes and cap each IP family at 4096 pairs; a full set cannot attribute additional pairs. Local router traffic, L2 traffic bypassing it, and offloaded flows may be invisible. Shared NAT addresses cannot distinguish teams; duplicate team IPs display N/A for network rates. Monitor each traffic path once to avoid counting the same packet on several exporters. See the [nftables reference](https://netfilter.org/projects/nftables/manpage.html) for table and counter semantics.

The FSP final network keeps participant WG addresses through the VPN server and SNATs only on the router's vulnbox-facing interfaces. Install this exporter on that router to see participant `10.200.0.0/24` addresses before SNAT. The current demo uses two container IPs in place of WG peers. The expanded team row shows the team's vulnbox address, while the Traffic source table shows the attacking peer's address. Neither view contains WG configs or private keys. Router Nginx and traffic to the ForcAD board need separate ingress metrics; these vulnbox counters do not measure them.

Prometheus alerts cover saturated DB/Redis pools, HTTP error ratio, WebSocket slots at 90%, slow-client drops, stale backend inventory, router counter and source-IP scrape failures, and configured router targets. An empty router target list creates no router-down alert. Alerts are visible in Prometheus; no Alertmanager is configured. Image tags are pinned to Prometheus v3.15.0 and Grafana v13.2.2, verified against the [Prometheus releases](https://github.com/prometheus/prometheus/releases) and [Grafana releases](https://github.com/grafana/grafana/releases) on 2026-10-12.

Run `make -C deployment test-monitoring` for the isolated Grafana/Prometheus checks and `make -C deployment test-network` for the nftables exporter checks. The latter grants NET_ADMIN only inside a disposable container with its own network namespace.

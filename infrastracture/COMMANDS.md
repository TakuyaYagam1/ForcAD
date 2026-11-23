# Commands

Run workstation commands from `infrastracture/` in the source tree and host
commands from the extracted role directory. Each archive contains this
reference, `APPLY.txt` and `UNINSTALL.txt`.

## Workstation

| Action | Command |
| --- | --- |
| Copy settings without overwriting | `cp -n settings.example.toml settings.toml` |
| Render a new deployment to `runtime/` | `python3 render.py` |
| Build ARM64 Router exporter | `python3 render.py --router-arch arm64` |
| Render monitoring add-on to a new directory | `python3 render.py --monitoring --config settings.toml --output monitoring-update` |
| Extend admins in an existing runtime | `python3 render.py --extend-admins --config settings.toml --output runtime` |
| Refresh Router WAN SSH in an existing runtime | `python3 render.py --refresh-router` |
| Run unit tests | `make test` |
| Run isolated checks | `make test-network`, `make test-game`, `make test-dashboards`, `make test-dependencies` |

Normal and `--monitoring` renders require a new output directory. Normal
rendering creates fresh keys; `--monitoring` uses deployed inventory and adds
no keys, game secrets or changes to the original runtime. For
`--extend-admins`, increase only top-level `admins` in `settings.toml` and use
the matching existing snapshot, admin server config and peer files. It adds
peers without rotating keys, updates `host.json` and the snapshot, and rebuilds
only `vpn.tar.gz`; it does not change live hosts. `--refresh-router` changes
only `router.wan_ssh_sources` or `router.wan_ssh_public`, updating the Router
nft file, archive and snapshot without key changes. Both update modes require
the existing runtime directory. `make test` uses the API virtualenv when
present, otherwise `python3`; set `PYTHON` to select another interpreter with
PyYAML.

## Extract and install

On each host, validate and extract its archive, then install and test from the
role directory. For Router:

```sh
gzip -t router.tar.gz
tar -xzf router.tar.gz
cd router
sudo bash install.sh
sudo bash test.sh
```

Repeat in order for `vpn.tar.gz`, then `forcad.tar.gz`. Run `sudo` from the
managing account. Managed files belong to `SUDO_USER`; direct root installs
remain root-owned. Secret files use mode `0600`; `/etc/wireguard` uses `0700`.
Keep console access during Router/VPN changes.

## Router

| Action | Command |
| --- | --- |
| Install/update | `sudo bash install.sh` |
| Enable network exporter (9109) | `sudo bash install.sh --network-metrics` |
| Enable Zeek exporter (9911) | `sudo bash install.sh --game-metrics` |
| Enable both exporters | `sudo bash install.sh --network-metrics --game-metrics` |
| Refresh exporters only | `sudo bash install.sh --monitoring` |
| Stage files only | `sudo bash install.sh --files-only` |
| Test live / files only | `sudo bash test.sh` / `sudo bash test.sh --files-only` |
| Preview / run removal | `sudo bash uninstall.sh --dry-run` / `sudo bash uninstall.sh` |
| Resume interrupted removal | `sudo bash uninstall.sh --resume --dry-run` / `sudo bash uninstall.sh --resume` |

`--game-metrics` ensures Zeek 9 or newer at `/opt/zeek` and creates
`forcad-sensor`. `--monitoring` manages only the monitoring table and exporter
services; it does not reapply base networking, keys, game phase or base
firewall/NAT. Reinstalling without flags retains selected exporters.

Pre-game permits participant ICMP echo only; game mode adds configured game
ports and each team's own management access. Both modes block inter-VLAN
traffic and direct vulnbox relays. Admin VPN has general Router forwarding;
participants have no general WAN egress. WAN forwards only configured
WireGuard UDP ports; non-public forwarded ranges are blocked.

To switch to game mode:

```sh
sudo nft -c -f /etc/forcad/router/game.nft
printf 'include "/etc/forcad/router/game.nft"\n' | sudo tee /etc/forcad/router/active.nft >/dev/null
sudo systemctl reload forcad-router-firewall.service
```

Use `pre-game.nft` in the same steps to switch back.

## VPN

Keep server configs private; client configs and `assignments.csv` stay on the
workstation. Give each client only its own config. For the same deployment,
preserve `wg-attack.conf` and `wg-admin.conf` and pass their directory with
`--wireguard-dir`; peer allocations must match. The installer may adopt
matching `/etc/wireguard` configs if none are selected, but refuses different
keys. Uninstall removes both managed server configs even when adopted.

| Action | Command |
| --- | --- |
| Install and start tunnels | `sudo bash install.sh` |
| Reuse server configs | `sudo bash install.sh --wireguard-dir /path/to/server` |
| Stage files only | `sudo bash install.sh --files-only` |
| Install stopped pcap / endpoint observer | `sudo bash install.sh --pcap` / `sudo bash install.sh --wg-endpoints` |
| Test files / live state | `sudo bash test.sh` / `sudo bash test.sh --live` |
| Test staged files only | `sudo bash test.sh --files-only` |
| Check status / preview removal | `sudo wg show` / `sudo bash uninstall.sh --dry-run` |
| Remove tunnels and server configs | `sudo bash uninstall.sh` |
| Start/stop installed pcap | `sudo systemctl start forcad-vpn-pcap.service` / `sudo systemctl stop forcad-vpn-pcap.service` |
| Start/stop installed endpoint history | `sudo systemctl start forcad-wg-endpoints.service` / `sudo systemctl stop forcad-wg-endpoints.service` |
| Check pcap status | `sudo python3 /usr/lib/forcad/vpn/vpnlib/observers.py pcap-status` |
| Forward private history viewer | `ssh -N -L 127.0.0.1:9837:/run/forcad-wg-history.sock operator@vpn-host` |

Observer flags stage stopped units and reload systemd only; they do not start
services, restart WireGuard, regenerate keys or apply networking. Pcap captures
both directions on `wg-attack` only between known peers and exact inventory
vulnboxes. The root-only ring in `/var/lib/forcad-vpn-pcap/` keeps up to 96
current and 96 prior files; it rotates every 300 seconds or at 4,000,000 kB per file. At 100 Mbit/s
it can store 3.75 GB per five minutes; total use can reach 786.5 GB / 732 GiB,
including file overshoot. Preflight requires about 377 GiB / 405 GB free,
including 512 MiB for exports and a 10 GiB reserve. Failure neither starts
capture nor prunes segments.

Export one TCP/UDP 5-tuple using inclusive UTC epoch milliseconds (six hours maximum):

```sh
sudo python3 /usr/lib/forcad/vpn/vpnlib/observers.py pcap-extract \
  --protocol tcp --source "$PEER_IP" --source-port "$PEER_PORT" \
  --destination "$BOX_IP" --destination-port "$SERVICE_PORT" \
  --from-ms "$FROM_MS" --to-ms "$TO_MS" --name incident-01
```

Exports allow 64 MiB each and eight files / 512 MiB total; existing exports
are not auto-deleted. No match is an error, not an empty file. Non-initial
fragments cannot match ports; packets above the 262,144-byte snap length are
truncated. A live segment may lack trailing frames; check completed segments
and drop counters.

Endpoint history polls every five seconds and records only inventory `/32`
peers with a valid endpoint and handshake no older than 180 seconds. `baseline`
records a snapshot, `first` a newly seen endpoint and `change` an IP change;
same-IP port rebinding is not a change. Stable peers refresh daily. Venue CIDRs
come from `/etc/forcad/vpn/endpoint-venues.txt`; missing, empty or invalid data
is `unknown`, and an IP outside valid venue ranges is `outside`. This is not
geolocation. It stores endpoint IP only, not port or keys. The root-only
database retains 30 days / 100,000 rows and 64 MiB; external IPs are absent
from stdout and metrics. The private viewer socket is `root:adm`, mode `0660`;
forward it as a member of remote `adm`, then open <http://127.0.0.1:9837/>.
Uninstall preserves captures and `/var/lib/forcad-wg-endpoints/history.sqlite3`.

## ForcAD

| Action | Command |
| --- | --- |
| Fresh install (no options) | `sudo bash install.sh` |
| Update monitoring on existing managed host | `sudo bash install.sh --monitoring` |
| Test install | `sudo bash test.sh` |
| Remove deployment | `sudo bash uninstall.sh --confirm` |
| Remove but keep packages/cache | `sudo bash uninstall.sh --confirm --keep-packages` |
| Remove and purge owned packages/data | `sudo bash uninstall.sh --confirm --purge-packages` |

The install path must be unused; Docker must have no `forcad` project
resources. The live address and gateway must match settings; installation does
not restart networking. Fresh `[env] monitoring = true` starts Prometheus and
Grafana; Router metrics settings add targets, not exporters.

The monitoring add-on preserves passwords and does not restart game services.
It requires a compatible managed checkout and does not migrate the game schema
or upgrade its binary. If Compose fails after staging, fix the error and rerun
`--monitoring`; staged passwords and targets are reused.

ForcAD removal has no dry-run and permanently deletes the checkout, database
and Compose volumes. Pre-existing packages are kept; purge stops if unrelated
Docker resources make ownership unclear. Do not prune the host.

## Router/VPN uninstall options

| Option | Effect |
| --- | --- |
| `--dry-run` | Preview Router/VPN removal. |
| `--files-only` | Stage/test files only; uninstall files without reverting manual live changes. |
| `--force` | Back up later edits to pre-existing shared files and restore originals. |
| `--discard-backups` | Remove rollback backups after forced uninstall. |
| `--resume` | Continue interrupted live Router uninstall with valid journal and manifest. |
| `--keep-packages` | Keep installer-added packages without prompting. |
| `--purge-packages` | Remove installer-added data; refuse unowned APT removals. |

Normal removal deletes newly created Router/VPN files, including edited copies,
and restores shared files from backup. Package prompts default to keep; failed
setup or purge retains ownership records. Keep records until cleanup succeeds.

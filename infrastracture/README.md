# Infrastructure

Generate settings and installation bundles for three dedicated Debian 12/13
hosts: a router, a WireGuard server and ForcAD. Team vulnboxes connect to the
router through one VLAN per team. Any hypervisor or physical hosts can be used.

`settings.toml` -> `render.py` -> host configs and archives -> host installers.
The renderer runs locally; only the installers change the destination hosts.

## Configure

On a workstation with Python 3.11+, run from the repository root:

```sh
cd infrastracture
cp -n settings.example.toml settings.toml
# Edit settings.toml before continuing.
```

[`settings.example.toml`](settings.example.toml) documents each field. Replace
example addresses, interfaces, endpoint and game settings with your own:

| Section | Controls |
| --- | --- |
| Top level | Admin VPN peers, game and management ports, DNS servers |
| `[network]` | Transit, ForcAD and VPN subnets, first team subnet and VLAN ID |
| `[router]` | WAN address and gateway, VPN/ForcAD links, team VLAN trunk |
| `[vpn]` | Host interface, public endpoint without a port, two UDP ports |
| `[forcad]` | Host interface, repository URL, branch or tag, unused install path |
| `[teams]` | Members per team; `[[teams.entries]]` lists names, logos and highlighting |
| `[env]` | ForcAD host address, public scheme, frontend branding, monitoring |
| `[game]` | Mode, rounds, timing, scores and flag lifetime |
| `[tasks]` | `[[tasks.entries]]` lists checkers, timeouts and action counts |

Team order determines consecutive `/24` networks and VLAN IDs. Each team also
gets a `/29` participant VPN block. The router takes the first usable address
in each transit, ForcAD and team subnet; the VPN transit interface and team
vulnboxes take the second. WireGuard interfaces take the first usable address
in their VPN ranges. `env.public_host` is the ForcAD host's IPv4 address inside
`network.forcad`. Overlapping networks are rejected.

Place team logos under `public/teams/`, the brand logo under `public/brand/`
and checkers under `checkers/` in the repository. Settings paths are relative
to those directories. Use an empty team logo when none is available. The bundle
copies selected logos, each selected checker's directory and shared checker
requirements. See [game configuration](../docs/CONFIGURATION.md) for field
semantics, including optional extra availability checks.

## Render and generate keys

From `infrastracture/`:

```sh
python3 render.py
python3 vpn/generate.py
```

The second command requires local `wireguard-tools`. It generates keys once
for a new deployment and adds server configs to `vpn.tar.gz`. `render.py`
creates no keys and does not clone or install ForcAD.

| Output under `runtime/` | Contents |
| --- | --- |
| `settings.toml`, `teams.json` | Input snapshot and calculated team addresses/VLANs |
| `router/` | Interfaces, routes, sysctl, firewall modes, systemd units and sensor configs |
| `vpn/` | Interfaces, routes, sysctl, firewall and service configs |
| `forcad/` | Complete `config.yml`, environment, checkers, logos and monitoring targets |
| `router.tar.gz`, `vpn.tar.gz`, `forcad.tar.gz` | Transfer bundles for the respective hosts |
| `wireguard/` | Server configs, individual client configs and `assignments.csv` |

Each host bundle includes `install.sh`, `test.sh`, `uninstall.sh`, `APPLY.txt`
and `UNINSTALL.txt`. The VPN archive includes only server keys under `server/`;
client configs stay in `wireguard/`. Send each client config only to its owner.
Directories use mode `0700`, files and archives `0600`. Keep `settings.toml`,
`runtime/` and keys private; these local paths are ignored by Git.

## Install and check

Prepare the hosts' interfaces and VLAN connections to match the settings.
Router and VPN require packages listed in their guides; ForcAD installs its
own prerequisites. Use clean, dedicated hosts. ForcAD requires an unused install
path and no existing ForcAD Docker project or volumes.

Install the router first, then VPN, then ForcAD. Copy each archive to its host
and extract it into a separate directory. For example, on the ForcAD host:

```sh
mkdir forcad
tar -xzf forcad.tar.gz -C forcad
cd forcad
sudo bash install.sh
sudo bash test.sh
```

Archives contain files at their root, without an enclosing directory. Use
`router` or `vpn` in this example for the other hosts. Run as root or use `sudo`.
Keep console access for network changes.

| Host | What `install.sh` does |
| --- | --- |
| [Router](router/README.md) | Saves original state, installs files, applies interfaces, routes, sysctl and firewall; starts selected exporters |
| [VPN](vpn/README.md) | Saves original state, installs server keys, applies networking, sysctl and firewall; starts `wg-attack` and `wg-admin` |
| [ForcAD](forcad/README.md) | Installs dependencies, clones the configured branch/tag, installs config/assets, initializes and starts the game and selected monitoring |

Router and VPN support `--files-only` to stage files without applying them.
ForcAD stages its interfaces file without restarting networking: its live
address and gateway must already match the settings. Follow the generated
`APPLY.txt` if manual network changes are needed. The ForcAD installer is for
first installation, not updates; the cloned revision must support the selected
game settings. Setup creates team tokens and admin credentials.

After all hosts are reachable, run `sudo bash test.sh` in each bundle. ForcAD
also runs its test during installation. Tests check installed files, syntax and
role-specific live services, routes and connectivity. Test VPN handshakes with
an actual client, team isolation, game/pre-game access, real checkers and load
separately. For Internet VPN access, forward both configured UDP ports through
any upstream NAT to the router; its generated rules forward them to the VPN.

## Monitoring

In `[env]`, `monitoring = true` starts Prometheus and Grafana with ForcAD metrics.
`router_metrics = true` and `game_metrics = true` add router exporter and Zeek
targets. Those flags configure scraping; install the matching router components
separately with `--network-metrics` and `--game-metrics`.

Network metrics need an inventory, nftables rules and a compatible ForcAD binary.
Game metrics need Zeek 9+ and the sensor account. Complete the base installation,
then follow the [router guide](router/README.md#network-metrics) to enable them.
Grafana and Prometheus bind to localhost; see the
[monitoring guide](../docs/MONITORING.md) for access and metric details.

## Change settings or remove an installation

The renderer refuses to overwrite output. To inspect changed settings:

```sh
python3 render.py --config settings.toml --output runtime/revision
```

This creates new configs and archives, not an update to a running game. Review
the differences before applying them. Preserve existing WireGuard keys and
check peer addresses against the new allocation; do not regenerate identities
just to change firewall settings. See the [VPN guide](vpn/README.md).

Router and VPN removal restores the recorded original files and live state:

```sh
sudo bash uninstall.sh --dry-run
sudo bash uninstall.sh
```

It only manages files recorded by the installer, including restoration of
files that existed before it ran. Manually copied files are not removed.
ForcAD uses a separate destructive removal command:

```sh
sudo bash uninstall.sh --confirm
```

This removes the managed checkout, database and Compose volumes, restores host
files and removes packages installed by its bootstrap. Read the bundle's
`UNINSTALL.txt` before removal. These scripts do not remove other deployments.

## Generator tests

From the repository root:

```sh
make -C infrastracture test
make -C infrastracture test-network
make -C infrastracture test-game
make -C infrastracture test-dashboards
```

The first target runs unit tests. The others require Docker and use disposable
containers. Validate reboot persistence and capacity on the intended hosts
before an event.

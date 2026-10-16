# Infrastructure

Deploy ForcAD on separate Debian 12/13 router, VPN and game hosts. Team
vulnboxes connect to the router through one VLAN per team. No hypervisor is
required.

## Generate bundles

Run on a workstation from this directory. Copy the example settings, edit
`settings.toml`, then render the host configs:

```sh
cp -n settings.example.toml settings.toml
python3 render.py
```

The TOML sets network ranges, interfaces, game rules, tasks, teams, branding and
monitoring. Replace the example ranges with subnets that do not overlap your WAN,
host LAN or other networks. `render.py` creates host bundles and `config.yml`;
it does not change host settings. Review the output before use.
Existing output is never overwritten. Use `--output runtime/revision` for a
separate render.

The render creates `runtime/router.tar.gz`, `runtime/vpn.tar.gz` and
`runtime/forcad.tar.gz`. Generate WireGuard keys before installing the VPN;
that step refreshes `vpn.tar.gz` with the required server configs. Output
directories use mode `0700`; files and archives use `0600`.

Generate WireGuard keys once for a new deployment:

```sh
python3 vpn/generate.py
```

This requires local `wireguard-tools`, refuses an existing output directory,
and refreshes `vpn.tar.gz` with the two server configs. Client configs remain
under `runtime/wireguard/`; distribute each only to its owner. Keep server keys
when changing network settings.

`settings.toml` and `runtime/` are ignored by Git. Never commit them.

## Install

Install in this order: router, VPN host, then ForcAD host. Keep console access
for router and VPN installation because network changes can disconnect SSH.
Each host bundle contains `install.sh`, `test.sh` and `uninstall.sh`.

Copy the matching archive to its host, extract it into a new directory, then
run `bash install.sh` from that directory. Install all three hosts before
running `bash test.sh` on each one. The ForcAD installer also runs its test
before reporting success.

Router and VPN installers require Debian 12/13 and their listed system tools;
they do not install OS packages. They check for conflicting network and
firewall state, save the original host state, then apply their settings.
Resolve a preflight refusal with the current owner of that configuration.

The ForcAD installer is only for a fresh deployment on a clean, dedicated host:
the configured install path and ForcAD Docker project must be unused. It installs
prerequisites, clones the configured revision and installs the rendered config
and assets. It does not update an existing game. Host network changes are staged;
review `host/APPLY.txt` and apply them from the console if needed.

For rollback, run `bash uninstall.sh --dry-run` first. Router and VPN uninstall
restore their recorded original state. ForcAD uninstall requires
`bash uninstall.sh --confirm` and permanently removes its database and Compose
volumes.

## Monitoring

Set `monitoring = true` in `[env]` to start Grafana and Prometheus with ForcAD
metrics. Set `router_metrics = true` to add the router exporter target and
`game_metrics = true` to add the Zeek sensor target. These flags configure
ForcAD scraping; the corresponding router components must also be installed.

For router network metrics, export `network.json`, `network.nft` and the
compatible `forcad` binary from the ForcAD host, place them beside the router
bundle, and install with `--network-metrics`. Game metrics require Zeek 9+ and
the `forcad-sensor` account on the router; install with `--game-metrics`.
See the [router guide](router/README.md) and [ForcAD guide](forcad/README.md).

Grafana and Prometheus bind to localhost. Use the [monitoring guide](../docs/MONITORING.md)
for SSH access and metric details.

`[network]` in `settings.toml` defines the router-VPN link, ForcAD subnet,
participant and admin VPN ranges, plus the first team subnet and VLAN ID. Each
team gets the next `/24`. The router uses the first host in the transit, ForcAD
and team subnets. The VPN server uses the first host in its ranges; the VPN host
and team vulnboxes use the second where applicable. Set `env.public_host` to the
ForcAD host's address inside its configured subnet. The router provides team
gateways and applies game or pre-game firewall rules.
Pre-game mode allows participant ping to vulnboxes; game mode allows configured
game ports and management access to the participant's own box. The VPN
preserves peer addresses. Client routes are generated from the configured
subnets; overlapping ranges are rejected.

## Checks

From this directory:

```sh
make test
make test-network
make test-game
make test-dashboards
```

`make test` runs unit tests. The other targets need Docker and use disposable
containers. These checks do not establish production capacity. Before an
event, verify VLAN isolation, reboot persistence, real checkers and expected
load on the intended hosts.

# Infrastructure

For event operators provisioning Debian 12 or 13 hosts: render Router, VPN
and ForcAD bundles from `settings.toml`, then install them in that order.

[Commands](COMMANDS.md#workstation) | [Router](router/README.md) |
[VPN](vpn/README.md) | [ForcAD](forcad/README.md)

Before provisioning, [size the ForcAD host](../docs/OPERATIONS.md#sizing-the-forcad-host).
Budget Router, VPN and each team's vulnbox separately.

## Configure and render

From the repository root:

```sh
cd infrastracture
cp -n settings.example.toml settings.toml
# Edit settings.toml first. The default output directory, runtime/, must not exist.
python3 render.py
```

Use [settings.example.toml](settings.example.toml) for field descriptions.
Configure 1-32 entries, with at least one player team and at most one NPC.
Team order assigns consecutive VLANs and one /24 per entry. Player teams get a
/29 VPN block for up to five members or a /28 for six or seven. An NPC gets a
VLAN and vulnbox address, but no participant VPN block or client identities.
Size the participant pool for player teams only, separate from the admin VPN.
Put logos under `public/teams/` or `public/brand/` and checkers under
`checkers/`; leave a logo value empty when unused.

Rendering needs Python 3.11+, Go at the version in `backend/go.mod` and
`wireguard-tools`. The Router exporter defaults to Linux/amd64; use
`--router-arch arm64` for ARM64. Rendering does not change a host. Normal
renders create new WireGuard keys and require an output directory that does
not exist.

For an existing runtime, use:

- `python3 render.py --monitoring --config settings.toml --output monitoring-update`
  creates three add-on archives in a new directory, without keys, game secrets
  or changes to the original runtime.
- `python3 render.py --extend-admins --config settings.toml --output runtime`
  adds admin peers to the existing runtime. Increase only top-level `admins`;
  retain matching snapshot, server config and old peer files. Keys are not
  rotated, only `vpn.tar.gz` is rebuilt, and live hosts are unchanged.
- `python3 render.py --refresh-router` updates only Router WAN SSH settings in
  the existing runtime. Only `router.wan_ssh_sources` or
  `router.wan_ssh_public` may change; keys and other archives are unchanged.

See the [command reference](COMMANDS.md#workstation) for exact render options.
The output contains three role archives, a settings snapshot, team allocations
and `runtime/wireguard/` with server and individual client configs.
`assignments.csv` maps teams, members and VPN addresses. Each archive extracts
into its role directory and includes `COMMANDS.md`, `APPLY.txt` and
`UNINSTALL.txt`. Keep archives and keys private; give each client only its own
VPN config. The VPN archive contains server configs, not client configs.

## Deploy

Copy and extract the archives to their hosts. Use the
[installation steps](COMMANDS.md#extract-and-install), keeping console access while
Router/VPN networking changes are applied. ForcAD needs an unused install path,
no existing `forcad` Docker resources and a live address/gateway matching the
rendered settings; its installer does not restart networking.

Run `sudo` from the account that manages the host. Managed files belong to
`SUDO_USER`; direct root installs remain root-owned. Secrets use mode `0600`
and `/etc/wireguard` uses `0700`. See [ForcAD monitoring access](../docs/MONITORING.md#start).

## Update and remove

For monitoring, set `[env] monitoring = true`. `router_metrics` and
`game_metrics` add ForcAD scrape targets; they do not install Router exporters.
See [monitoring](../docs/MONITORING.md) and the
[ForcAD](forcad/README.md#install-and-test) and
[Router exporter](router/README.md#exporters) instructions.

To reinstall the same VPN deployment, preserve deployed server configs and
pass their directory with `install.sh --wireguard-dir PATH`. VPN uninstall
removes those configs even when adopted from `/etc/wireguard`; keep private
copies. ForcAD removal requires `--confirm`, has no dry-run and permanently
deletes the managed checkout, database and Compose volumes. See
[COMMANDS.md](COMMANDS.md) for cleanup options.

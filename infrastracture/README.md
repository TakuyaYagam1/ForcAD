# Infrastructure

The deployment flow is `settings.toml` -> render -> three host archives ->
install Router, VPN and ForcAD. Hosts must run Debian 12 or 13. Prepare their
network interfaces and VLANs for the configured address plan.

[Command reference](COMMANDS.md) | [Router](router/README.md) |
[VPN](vpn/README.md) | [ForcAD](forcad/README.md)

## Configure and render

For the 28-team final, use [settings.final.example.toml](settings.final.example.toml)
and the [capacity and rehearsal guide](../deployment/final/README.md). Replace
the placeholder checkers and network values before rendering.

From the repository root:

```sh
cd infrastracture
cp -n settings.example.toml settings.toml
# Edit settings.toml, then render.
python3 render.py
```

Use [settings.example.toml](settings.example.toml) for field descriptions. Set
the addresses, interfaces, game settings, tasks, teams and monitoring options
for your deployment. Team order assigns consecutive VLANs and one /24 network
per entry. Configure 1-32 entries, with at least one player team and at most one
NPC. Player teams receive one /29 participant VPN block for up to five members
or one /28 block for six or seven members. For 28 player teams, those larger
blocks require a /23 participant VPN pool that does not overlap the admin VPN.
Add one `[[teams.entries]]` with `kind = "npc"` for a dedicated test target (for
example, entry 19 after 18 player teams). It receives a VLAN and vulnbox
address in sequence, but no participant VPN block or client identities. Size
the participant VPN pool for player teams only and keep it separate from the
admin VPN. Deploy the same services on this NPC vulnbox and ensure participants
can reach its service IP through the attack VPN. Put logos under
`public/teams/` or `public/brand/`, and checkers under `checkers/`. Leave a logo
value empty when none is needed.

Rendering requires Python 3.11 or newer, Go at the version specified in
`backend/go.mod`, and `wireguard-tools`. It builds the Router exporter for
Linux/amd64 by default; use `--router-arch arm64` for an ARM64 Router. Rendering
does not change a host.

The output directory must not exist. The renderer creates fresh WireGuard keys
each time, so render to a new directory for a new deployment.

| Output | Contents |
| --- | --- |
| `runtime/router.tar.gz` | Router bundle: network and firewall files, exporter, inventory and sensor files |
| `runtime/vpn.tar.gz` | VPN bundle with `wg-attack` and `wg-admin` server configs |
| `runtime/forcad.tar.gz` | ForcAD config, environment, checkers, logos and monitoring targets |
| `runtime/wireguard/` | Server and individual client configs; `assignments.csv` lists team, member and VPN addresses |
| `runtime/settings.toml`, `runtime/teams.json` | Input snapshot and allocated team addresses |

Each archive extracts into its role directory and includes `COMMANDS.md`,
`APPLY.txt` and `UNINSTALL.txt`. Keep runtime output, archives and keys private.
Give each client only its own VPN config. The VPN archive contains server
configs, not client configs.

## Install and test

Copy each archive to its host and extract it. The archive creates `router/`,
`vpn/` or `forcad/`. Install Router first, then VPN, then ForcAD. Run each
role's `test.sh` after its dependencies are reachable. See
[COMMANDS.md](COMMANDS.md) for exact commands and options.

Router and VPN apply network settings, so keep console access during
installation. ForcAD requires an unused install path and no existing ForcAD
Docker project or volumes. Its live address and gateway must
already match the rendered settings; the installer does not restart networking.

Run installation with `sudo` from the account that will manage the host. The
installer assigns managed Router/VPN files and the ForcAD checkout, including
WireGuard keys, to `SUDO_USER`. Reinstalls retain the recorded owner; a direct
root install remains root-owned. Secret files use mode `0600`, and
`/etc/wireguard` uses `0700`. Database/container storage and recovery records
retain service or root ownership.

Router and VPN install missing host prerequisites automatically. If Python is
absent, their shell wrappers bootstrap it. `--files-only` still installs
prerequisites and requested metrics dependencies, but stages files without
applying network settings or managed services.

## Monitoring

In `[env]`, set `monitoring = true` to generate a Grafana password and start the
Prometheus/Grafana overlay on ForcAD. Set `router_metrics` or `game_metrics` only
when that Router exporter will also be installed: these settings add the
corresponding Prometheus target on ForcAD, but do not change the Router.

Enable the exporters separately on the Router with
`install.sh --network-metrics` and/or `install.sh --game-metrics`. The network
exporter is bundled. Game metrics use Zeek 9 or newer at `/opt/zeek`; the
installer adds Zeek when needed and creates the `forcad-sensor` account. Both
exporters listen on the Router address facing ForcAD. The Router explicitly
permits the ForcAD host to reach those ports; admin VPN clients already have
general Router access. Prometheus and Grafana bind to localhost on ForcAD. See
[monitoring access](../docs/MONITORING.md).

## Default traffic policy

The Router starts in pre-game mode and forwards only the configured WireGuard
UDP ports from WAN. Direct inter-vulnbox forwarding stays blocked, while
vulnboxes retain public WAN egress. See the [Router firewall
policy](router/README.md#firewall-and-removal).

## Re-render and uninstall

To render changed settings, use a new output directory, such as
`python3 render.py --output runtime-next`. This creates new WireGuard identities
and does not update a running game or reuse keys from an existing output
directory. For the same deployment, keep the deployed server configs and select
their directory with `--wireguard-dir` when installing the VPN bundle.

Router and VPN uninstallers restore recorded shared host files and state. The
VPN uninstaller removes `wg-attack` and `wg-admin` configs even when it
adopted them from `/etc/wireguard`; keep copies if you may reinstall.
Package removal is optional and limited to dependencies added by that
installer; pre-existing packages are kept. A staged `--files-only` install
does not record manual live changes for automatic removal.

ForcAD uninstall requires `--confirm`, has no dry-run, and permanently deletes
the managed checkout, database and Compose volumes. Package and Docker cleanup
is optional. Pre-existing packages are kept; purge stops if unrelated Docker
resources make ownership unclear. See the command reference for cleanup options.

The source-tree checks are listed in [COMMANDS.md](COMMANDS.md#workstation).

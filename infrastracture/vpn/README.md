# VPN

Generate the bundle with the [main guide](../README.md). See [commands and
options](../COMMANDS.md#vpn).

## Host and keys

Use a dedicated Debian 12 or 13 host. The installer adds missing packages,
including WireGuard tools, nftables and ifupdown. Keep console access because
network changes can interrupt SSH.

Rendering generates `wg-attack` and `wg-admin` server configs and client
configs. The VPN archive contains the server configs. Client configs and
`assignments.csv` stay under `runtime/wireguard/` on the workstation; give each
client only its own config. Keep archives and keys private.

A render to a new output directory creates new keys. For the same deployment,
keep its server configs and pass their directory with `--wireguard-dir`. Peer
addresses must still match the generated allocation.
Without bundled or selected configs, install can reuse `/etc/wireguard/`, but
refuses different keys.

## Install and remove

The installer applies networking, sysctl and firewall, then starts both tunnels.
The Router must forward the configured VPN UDP ports and route both VPN ranges
through this host. The VPN host preserves peer addresses without SNAT.

The test checks installed files, tunnels, routes and configured connectivity.
Verify an actual client handshake and access isolation separately. Uninstall
stops both managed tunnels and removes their server configs, including configs
adopted from `/etc/wireguard/`. Other WireGuard configs remain untouched. Keep a
private copy of the deployed server configs if you may reinstall.

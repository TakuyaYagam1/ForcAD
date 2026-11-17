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

Every render into a new output directory creates new server and client keys.
For the same deployment, preserve the deployed server configs and pass their
directory to `install.sh --wireguard-dir PATH`; their peer allocations must
match the bundle. If neither bundled nor selected configs are available, the
installer may adopt matching `wg-attack.conf` and `wg-admin.conf` from
`/etc/wireguard`, but refuses different keys.

## Install and remove

The installer applies networking, sysctl and firewall, then starts both tunnels.
The VPN host preserves peer addresses without SNAT. The Router must route both
VPN ranges through this host; see the [Router
guide](../router/README.md#firewall-and-removal) for WAN forwarding.

Participant peers may reach peers in their own team's VPN block and the Router
transit. The VPN firewall blocks participant traffic to other team blocks and
the admin VPN. Admin peers are not subject to those participant restrictions.

The test checks installed files, tunnels, routes and configured connectivity.
Verify an actual client handshake and access isolation separately. Uninstall
stops both managed tunnels and removes their server configs, including configs
adopted from `/etc/wireguard/`. Other WireGuard configs remain untouched. Keep a
private copy of the deployed server configs if you may reinstall.

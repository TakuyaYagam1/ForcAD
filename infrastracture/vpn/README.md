# VPN

Event operators install the VPN server bundle on a dedicated Debian 12 or 13
host and preserve keys when reusing a deployment. See the
[command reference](../COMMANDS.md#vpn) for observer operations.

## Install and test

Extract `vpn.tar.gz`; from the `vpn/` directory:

```sh
sudo bash install.sh
sudo bash test.sh --live
```

The installer applies networking, sysctl and firewall settings, then starts
both tunnels. Keep console access because network changes can interrupt SSH.
The Router must route both VPN ranges through this host. Participant peers can
reach their own team VPN block and Router transit, not other team blocks or
the admin VPN. Admin peers are not subject to those restrictions.

## Preserve keys and remove safely

The archive contains `wg-attack` and `wg-admin` server configs. Client configs
and `assignments.csv` stay under `runtime/wireguard/` on the workstation; keep
them private and give each client only its own config. Rendering to a new
output directory creates new server and client keys.

To reinstall the same deployment, preserve server configs and pass their
directory with `install.sh --wireguard-dir PATH`; peer allocations must match.
If no selected or bundled configs are available, the installer may adopt
matching `wg-attack.conf` and `wg-admin.conf` from `/etc/wireguard`, but refuses
different keys.

Preview, then remove:

```sh
sudo bash uninstall.sh --dry-run
sudo bash uninstall.sh
```

Uninstall stops both managed tunnels and removes their server configs, even
those adopted from `/etc/wireguard`; keep a private copy for reinstall. Other
WireGuard configs remain untouched. Optional `--pcap` and `--wg-endpoints`
observers are installed stopped and do not reconfigure VPN. Uninstall removes
their units but preserves capture and endpoint-history data. See
[COMMANDS.md#vpn](../COMMANDS.md#vpn) for start, test and retention details.

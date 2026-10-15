# Router

Debian 12/13 with `nftables`, `iproute2`, `ifupdown`, `vlan` and `resolvconf`.
Set NIC names, WAN settings and service ports in `network.toml`, then
[generate configs](../README.md#generate-configs).

## Install

Keep console access. Back up interfaces, sysctls, firewall files, enabled units
and `nft list ruleset`. Merge `runtime/router/interfaces` into
`/etc/network/interfaces`; preserve management access and use one network manager
per device. Configure DNS separately if not using resolvconf.

Set the switch router port to trunk VLANs 101 through `100 + teams`. Each guest
port gets one access VLAN. VPN return routes are included in `interfaces`.

Copy `runtime/router/` to the router. Run from that directory:

```sh
sudo install -d -m 0750 /etc/forcad/router
sudo install -m 0600 common.nft game.nft pre-game.nft active.nft /etc/forcad/router/
sudo install -m 0644 sysctl.conf /etc/sysctl.d/90-forcad-router.conf
sudo install -m 0644 forcad-router-firewall.service /etc/systemd/system/
sudo install -d /etc/systemd/system/networking.service.d
sudo install -m 0644 networking.conf /etc/systemd/system/networking.service.d/forcad.conf
sudo nft -c -f /etc/forcad/router/active.nft
sudo systemctl daemon-reload
sudo systemctl enable --now forcad-router-firewall.service
sudo sysctl -p /etc/sysctl.d/90-forcad-router.conf
```

Apply interfaces from the console. The initial mode is pre-game. The networking
drop-in requires the firewall before ifupdown starts. IPv4 forwarding is enabled;
redirects and IPv6 are disabled.

Use one firewall manager. Migrate any required stock nftables/UFW/firewalld rules;
do not let another service run `flush ruleset`. Stopping `nftables.service` may
flush rules too: inspect it before disabling its boot activation.

## Change game mode

Firewall mode is independent of the ForcAD schedule. To select game mode and
persist it across reboot:

```sh
sudo nft -c -f /etc/forcad/router/game.nft
printf 'include "/etc/forcad/router/game.nft"\n' | sudo tee /etc/forcad/router/active.nft >/dev/null
sudo systemctl reload forcad-router-firewall.service
```

Use `pre-game.nft` for ping-only participant access to vulnboxes, including their
own. Board, admin and checker access stays available. See the
[access policy](../README.md#access-policy).

Reload atomically replaces only `inet forcad_router` and `ip forcad_nat`.
Monitoring counters survive. Failed application leaves old kernel rules active;
restore the previous `active.nft` before reboot if a reload fails.

Pre-game blocks existing participant TCP/UDP flows too. Conntrack is retained,
so surviving connections may resume in game mode. Test NAT changes with new
connections.

## Network metrics

Export `network.json` and `network.nft` on the
[ForcAD host](../forcad/README.md#monitoring). Copy them and a router-compatible
`forcad` binary beside the generated service files. Run there:

```sh
sudo install -d -m 0750 /etc/forcad/monitoring
sudo install -m 0600 network.json network.nft /etc/forcad/monitoring/
sudo install -m 0755 forcad /usr/local/bin/forcad
sudo install -m 0644 network-rules.service network-metrics.service /etc/systemd/system/
sudo nft -c -f /etc/forcad/monitoring/network.nft
sudo systemctl daemon-reload
sudo systemctl enable --now network-metrics.service
```

- `network-rules.service` creates the counter table once per boot. Do not apply
  `network.nft` manually first or restart this unit to reload counters.
- `network-metrics.service` reads counters and serves `10.183.2.1:9109`. Restarting
  this process does not reset counters.
- Only ForcAD and admins can reach the listener. `nft` must be in PATH.

To update the inventory, stage matching JSON/rules and back up the old pair.
Validate and apply one nft transaction containing `delete table inet
forcad_monitoring` followed by the new rules; then restart
`network-metrics.service`. This resets counters, not access rules. Never flush the
whole ruleset.

## Game HTTP/WS metrics

Install Zeek 9.0 using the [official Debian package instructions](https://docs.zeek.org/en/v9.0.0/install.html#binary-packages).
Check `/opt/zeek/bin/zeek --version`; adjust the unit's binary path if needed.
The scripts are tested with Zeek 9.0.0.

The sensor captures both directions on the configured VPN-facing interface
(`ens20` in the example), before filtering and SNAT. Do not also capture `any`,
the trunk, or run ZeekControl on that path: duplicate capture double-counts
traffic.

From the generated router directory, create the account once and install:

```sh
sudo useradd --system --user-group --no-create-home --shell /usr/sbin/nologin forcad-sensor
sudo install -d -o root -g forcad-sensor -m 0750 /etc/forcad/monitoring
sudo install -o root -g forcad-sensor -m 0640 game-metrics.zeek game-inventory.zeek /etc/forcad/monitoring/
sudo install -m 0644 game-metrics.service /etc/systemd/system/
sudo -u forcad-sensor /opt/zeek/bin/zeek -b -a /etc/forcad/monitoring/game-metrics.zeek /etc/forcad/monitoring/game-inventory.zeek
sudo systemctl daemon-reload
sudo systemctl enable --now game-metrics.service
```

Inspect any existing `forcad-sensor` account before reusing it. Install the
matching router rules for private TCP/9911 access. Verify the endpoint from
ForcAD and add its target as described in the [ForcAD guide](../forcad/README.md#monitoring).

The unit runs with NET_RAW, no root/NET_ADMIN, a 1 GiB memory limit and one CPU.
These limits need load testing. Sensor failure does not stop forwarding.
See [Monitoring](../../docs/MONITORING.md#game-traffic) for metric coverage.

After changing peers, team IPs or ports, regenerate the inventory/unit together
and restart the sensor. Restart resets counters and tracked sessions. Roll back
by restoring its files, or disable the unit and remove its Prometheus target;
forwarding rules need no changes.

## Verify and roll back

Check game/pre-game access, target-side SNAT, Internet egress, blocked direct
vulnbox connections, admin access and real checker CHECK/PUT/GET. Repeat after
reboot. The full [pre-event checks](../README.md#verification) also cover load.

On failure, restore saved interfaces, sysctls and firewall files from the console,
then reload the affected units. Restore only changed tables; a full ruleset
snapshot can overwrite rules owned by other software.

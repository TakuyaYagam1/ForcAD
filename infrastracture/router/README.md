# Router

Generate the bundle with the [main guide](../README.md). See [commands and
options](../COMMANDS.md#router).

## Host requirements

Use a dedicated Debian 12 or 13 host with static IPv4 configured through
ifupdown. Prepare the interfaces, VLAN trunk and address plan from
`settings.toml`. Resolve DHCP, active IPv6 and other firewall ownership before
installation. Do not flush rules to bypass installer checks.

The installer adds missing base packages. If Python is absent, `install.sh`
bootstraps it. Keep console access because network changes can interrupt SSH.

## Exporters

The bundle includes the network exporter and inventory files; no Go install is
needed on the Router. Enable it with `--network-metrics`.

Game metrics are optional. `--game-metrics` requires Zeek 9 or newer at
`/opt/zeek`. The installer adds Zeek if needed and creates the `forcad-sensor`
account. It captures the configured VPN-facing interface before SNAT. Use both
options to enable both exporters.

## Firewall and removal

The initial firewall mode is pre-game. Owned rules live under
`/etc/forcad/router/` in nftables tables `inet forcad_router` and `ip
forcad_nat`. Forward the configured VPN UDP ports to the Router WAN address. See
[commands and options](../COMMANDS.md#router) to switch modes and uninstall.

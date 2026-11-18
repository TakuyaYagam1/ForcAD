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

The bundle contains the network exporter binary and inventory; the Router does
not need Go. Enable it with `install.sh --network-metrics`. It installs a
dedicated nftables counter table and listens on port 9109 of the Router address
facing ForcAD.

Enable passive game traffic metrics with `install.sh --game-metrics`. This
ensures Zeek 9 or newer at `/opt/zeek`, creates the `forcad-sensor` system
account when needed, and captures the VPN-facing interface before team-destination
SNAT. Its exporter listens on port 9911 of the Router address facing ForcAD.
The base firewall explicitly permits the ForcAD host to reach both exporter
ports; admin VPN clients already have general Router access. Use both install
options to enable both exporters; reinstalling without flags retains previously
selected exporters.

## Firewall and removal

The firewall starts in pre-game mode. It permits participant ICMP echo to
vulnboxes but blocks other participant-to-vulnbox traffic. Game mode permits
configured game ports to every vulnbox and configured management ports only
from each player team's own participant VPN block. Both modes block forwarding
between team VLANs and direct vulnbox relays.

WAN ingress DNATs only the configured WireGuard UDP ports to the VPN host. If
this Router is behind upstream NAT, forward those ports to its configured WAN
address. Vulnboxes, ForcAD and the VPN host may initiate connections to public
WAN destinations and are masqueraded; their forwarded traffic to non-public
ranges is blocked. Admin VPN clients have unrestricted Router forwarding.
Participant VPN clients have no general WAN egress.

Router SSH on WAN is disabled by default. List specific management host IPv4
addresses in `router.wan_ssh_sources` to allow TCP/22 only from those sources
to the Router's WAN IP. To allow direct SSH from arbitrary public IPs, set
`router.wan_ssh_public = true` instead. This opens only TCP/22 to the WAN IP;
other input stays drop. Use key-only SSH and disable password authentication
before enabling this public option.

Owned rules live under `/etc/forcad/router/` in nftables tables `inet
forcad_router` and `ip forcad_nat`. See [commands and
options](../COMMANDS.md#router) to switch modes and uninstall.

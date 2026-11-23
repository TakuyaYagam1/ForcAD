# Router

Event operators install the Router bundle on a dedicated Debian 12 or 13 host
to route team and admin VPN traffic. See the [command reference](../COMMANDS.md#router)
for options and removal.

## Host requirements

Configure static IPv4 with ifupdown, interfaces, VLAN trunk and address plan
from `settings.toml` before installation. Resolve DHCP, active IPv6 and existing
firewall ownership first; do not flush rules to bypass installer checks.
Packages are installed as needed. Keep console access because network changes
can interrupt SSH.

From the extracted `router/` directory:

```sh
sudo bash install.sh
sudo bash test.sh
```

## Exporters

The bundle includes the network exporter; the Router does not need Go.
`--network-metrics` enables it on TCP/9109 facing ForcAD.
`--game-metrics` enables passive Zeek on TCP/9911. It requires Zeek 9 or newer
at `/opt/zeek`; the installer bootstraps it when needed and creates the
`forcad-sensor` account. Zeek captures on the VPN-facing interface before
team-destination SNAT.

`/etc/forcad/monitoring/network.json` contains team addresses, not credentials.
It uses mode `0644` so the capability-restricted exporter can read it. Both
flags enable both exporters; reinstalling without flags retains selected
exporters.

On an existing Router, `sudo bash install.sh --monitoring` installs or refreshes
both exporters and manages the monitoring table. It does not reapply base
networking, WireGuard keys, game phase or base firewall/NAT. Repeat runs are
safe. Prometheus on ForcAD also needs matching scrape targets; see
[monitoring](../../docs/MONITORING.md).

The Zeek bootstrap accepts only these OBS `Release.key` SHA-256 values:

- Debian 12: `b67ae73b8543af4195c930b52a07c208611253fdc519873adcf1190ffcfc1377`
  or `909acf470dcfc539f362af9a4b565bb803702d579976a238fbe2eef284a78a1e`.
- Debian 13: `909acf470dcfc539f362af9a4b565bb803702d579976a238fbe2eef284a78a1e`.

Both contain RSA primary-key fingerprint
`F9FA0223B56B116C363737EF5DA57BDD6DD785CA`. See the
[OBS signing-key page](https://build.opensuse.org/projects/security:zeek/signing_keys)
and [Zeek installation guide](https://docs.zeek.org/en/current/install.html).
Do not bypass the artifact pin or APT signature checks.

## Firewall and removal

Pre-game permits participant ICMP echo to vulnboxes and blocks other
participant-to-vulnbox traffic. Game mode permits configured game ports to all
vulnboxes and configured management ports only from each player team's VPN
block. Both modes block inter-team VLAN traffic and direct vulnbox relays.

WAN ingress forwards only configured WireGuard UDP ports. Vulnboxes, ForcAD and
the VPN host may initiate public WAN connections; forwarded traffic to
non-public ranges is blocked. Admin VPN clients have general Router forwarding;
participants have no general WAN egress.

WAN SSH is disabled by default. `router.wan_ssh_sources` allows TCP/22 only from
listed management IPv4 addresses; `router.wan_ssh_public = true` allows it from
any public IP. Other WAN input is dropped. Before enabling public SSH, require
key-only authentication and disable password authentication.

Owned nftables tables are `inet forcad_router` and `ip forcad_nat`, under
`/etc/forcad/router/`. Preview or run removal using
[COMMANDS.md#router](../COMMANDS.md#router).

# Router

Use a dedicated Debian 12/13 host with `python3`, `nftables`, `iproute2`,
`iputils-ping`, `procps`, `ifupdown`, `vlan` and `resolvconf` installed. The
installer does not install packages. Set interfaces, addresses and game ports
in [`settings.toml`](../settings.example.toml), then run `python3 render.py` in
the infrastructure directory.

## Install

The router installer changes interfaces, routes, sysctl settings, nftables and
selected monitoring services. Run it from the host console; it can interrupt
SSH. It supports static IPv4 with ifupdown. DHCP, active IPv6 and firewall rules
owned by another service must be resolved before installation. Do not disable
another firewall or flush its rules to bypass a preflight refusal.

Copy `runtime/router.tar.gz` to the router and extract it into a new directory.
Run there:

```sh
bash install.sh
bash test.sh
```

The installer records the original host state in
`/var/backups/forcad/router/live.json`. Reinstalls keep this baseline and the
selected game mode. Use `--files-only` to stage files without changing live
network or service state. The matching `test.sh --files-only` checks staged
files only.

The installer owns `inet forcad_router` and `ip forcad_nat`. It does not flush
the full nftables ruleset. A pre-existing firewall manager or custom rules may
cause preflight to stop; review them first.

## Monitoring

For network metrics, place `network.json`, `network.nft` and the
router-compatible `forcad` binary beside the extracted bundle, then run:

```sh
bash install.sh --network-metrics
```

For HTTP/WS and flow metrics, install Zeek 9+ under `/opt/zeek`, create the
`forcad-sensor` system account, then run:

```sh
bash install.sh --game-metrics
```

The sensor captures both directions on the VPN-facing interface before
filtering and SNAT. Do not capture the trunk or `any`; duplicate capture counts
traffic twice. For both exporters, provide both options to `install.sh`.
ForcAD must also be configured to scrape enabled targets in `settings.toml`.

## Firewall mode and rollback

The initial mode is pre-game. To select game mode:

```sh
sudo nft -c -f /etc/forcad/router/game.nft
printf 'include "/etc/forcad/router/game.nft"\n' | sudo tee /etc/forcad/router/active.nft >/dev/null
sudo systemctl reload forcad-router-firewall.service
```

For removal, preview first, then restore the recorded baseline:

```sh
bash uninstall.sh --dry-run
bash uninstall.sh
```

File-only installs have no live rollback record. See [UNINSTALL.txt](UNINSTALL.txt)
before removing them or using `--force`.

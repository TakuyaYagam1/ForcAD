# Commands

Run workstation commands from `infrastracture/` in the source tree. Run host
commands from the root of the extracted role directory. Each archive includes
this file.

## Workstation

These commands require the source tree.

| Command | Action |
| --- | --- |
| `cp -n settings.example.toml settings.toml` | Create local settings without replacing an existing file. Edit it before rendering. |
| `python3 render.py` | Read `settings.toml`, build the Router exporter, generate WireGuard configs and write bundles and archives under `runtime/`. |
| `python3 render.py --router-arch arm64` | Build the Router exporter for Linux/ARM64. The default is Linux/amd64. |
| `python3 render.py --config settings.toml --output runtime-next` | Render from the named config into a new output directory. This creates fresh WireGuard keys. |
| `make test` | Run infrastructure unit tests. |
| `make test-network`, `make test-game`, `make test-dashboards` | Run the isolated network, game and dashboard checks. |
| `make test-dependencies` | Check Debian package setup, Python bootstrap and interrupted cleanup recovery in Docker. |

The render output directory must not exist. A new output creates a new
deployment with new keys. To reinstall the same VPN deployment, preserve its
server configs and pass their directory to `install.sh --wireguard-dir PATH`.

## Extract an archive

Each archive contains its role directory. For example:

```sh
gzip -t router.tar.gz
tar -xzf router.tar.gz
cd router
```

Use `vpn.tar.gz` and `forcad.tar.gz` for the other hosts. Install in this order:
Router, VPN, ForcAD.

Run installation with `sudo` from the account that will manage the host. The
installer records `SUDO_USER` as owner of host-bundle files and the ForcAD
checkout; direct root installs stay root-owned and reinstalls keep the recorded
owner. Secret files use mode `0600`. The WireGuard directory uses mode `0700`.

## Router

Router install applies network settings, sysctl, firewall and selected
exporters. The default firewall mode is pre-game.

| Command | Action |
| --- | --- |
| `sudo bash install.sh` | Install prerequisites and apply Router settings. On reinstall, keep previously selected exporters. |
| `sudo bash install.sh --network-metrics` | Enable the bundled network exporter on port 9109. |
| `sudo bash install.sh --game-metrics` | Ensure Zeek 9 or newer at `/opt/zeek`, create `forcad-sensor` if needed, and enable game metrics on port 9911. |
| `sudo bash install.sh --network-metrics --game-metrics` | Enable both exporters. |
| `sudo bash install.sh --files-only` | Stage files without applying network settings or managed services. Prerequisites and requested metrics packages may still be installed. |
| `sudo bash test.sh` | Check files and live Router state, including addresses, routes, services and configured connectivity. |
| `sudo bash test.sh --files-only` | Check staged files without checking live state. |
| `sudo bash uninstall.sh --dry-run` | Preview removal. |
| `sudo bash uninstall.sh` | Restore saved files and live host state. |
| `sudo bash uninstall.sh --resume --dry-run` / `sudo bash uninstall.sh --resume` | Preview or continue an interrupted live Router uninstall with a valid journal and file manifest. |

To switch to game mode, validate the file, change the active include and reload
the firewall:

```sh
sudo nft -c -f /etc/forcad/router/game.nft
printf 'include "/etc/forcad/router/game.nft"\n' | sudo tee /etc/forcad/router/active.nft >/dev/null
sudo systemctl reload forcad-router-firewall.service
```

Use `pre-game.nft` in the same steps to switch back.

## VPN

The standard bundle contains `server/wg-attack.conf` and `server/wg-admin.conf`.
Keep those files private. The client configs stay under `runtime/wireguard/` on
the workstation; give each client only its own config.

| Command | Action |
| --- | --- |
| `sudo bash install.sh` | Install prerequisites, apply networking and firewall, then start both tunnels. |
| `sudo bash install.sh --wireguard-dir /path/to/server` | Use existing server configs for the same deployment. Peer addresses must match the rendered allocation. |
| `sudo bash install.sh --files-only` | Stage files without applying network settings or managed services. Prerequisites are still installed. |
| `sudo bash test.sh` | Check files, tunnels, routes and configured connectivity. |
| `sudo bash test.sh --files-only` | Check staged files only. |
| `sudo wg show` | Check WireGuard status and client handshakes. |
| `sudo bash uninstall.sh --dry-run` | Preview removal. |
| `sudo bash uninstall.sh` | Stop the managed tunnels, remove their server configs and restore saved host state. |

Without bundled or selected server configs, install may reuse `/etc/wireguard/`.
Uninstall removes `wg-attack.conf` and `wg-admin.conf` even when it reused them.
Other WireGuard configs are preserved.

## ForcAD

Use the top-level scripts. Scripts under `host/` are helpers called by the
installer.

| Command | Action |
| --- | --- |
| `sudo bash install.sh` | Install prerequisites, clone the configured revision, initialize the game configuration, and start the application and selected monitoring. Takes no options. |
| `sudo bash test.sh` | Validate the game configuration and check host files, services and selected monitoring. |
| `sudo bash uninstall.sh --confirm` | Permanently delete the managed game, database and volumes, then ask whether to purge installer-added dependencies and owned Docker data. |
| `sudo bash uninstall.sh --confirm --keep-packages` | Delete the deployment and keep system packages and Docker images/build cache. |
| `sudo bash uninstall.sh --confirm --purge-packages` | Delete the deployment and remove installer-added packages and install-owned Docker data, subject to ownership checks. |

ForcAD has no uninstall dry-run. Pre-existing packages are never removed. Purge
stops if unrelated Docker resources make ownership unclear. The downloaded Go
toolchain and caches are inside the managed checkout and are removed in either
package mode.

## Router/VPN uninstall options

| Option | Effect |
| --- | --- |
| `--dry-run` | Preview file and live-state removal. |
| `--files-only` | For install, stage files without applying live settings. For test, check files only. For uninstall, remove files without reverting manually applied live changes. Use only for a staged install without a live recovery journal. |
| `--force` | Back up and restore a pre-existing shared file changed after installation. This does not bypass ownership or recovery checks. |
| `--discard-backups` | Remove completed rollback backups after a successful forced uninstall. |
| `--resume` | Continue a live Router uninstall with a valid journal and installed file manifest. Router only. |
| `--keep-packages` | Keep installer-added packages without prompting. Ownership records remain for a later purge. |
| `--purge-packages` | Remove installer-added packages and recorded data without prompting. The purge is refused if APT would remove an unowned package. |

New files created by Router or VPN install are removed during normal uninstall,
including edited copies. Pre-existing shared files are restored from their
install backup. The VPN-owned `wg-attack.conf` and `wg-admin.conf` files are
removed even when adopted from `/etc/wireguard`; keep copies if you may
reinstall. Router and VPN ask whether to purge installer-added dependencies
when no package option is given; the default is to keep them. Pre-existing
packages are not recorded for removal. A failed setup or purge leaves ownership
records for retry. Keep the records until cleanup succeeds.

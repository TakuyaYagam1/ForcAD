# ForcAD

Generate the bundle with the [main guide](../README.md). See [commands and
options](../COMMANDS.md#forcad).

## Host requirements

Use Debian 12 or 13. Its live address and gateway must match `settings.toml`.
The install path must be unused, and Docker must not contain containers,
volumes or networks for the `forcad` Compose project. This installer creates a
new game; it does not update an existing deployment.

The installer does not resize VMs. Use the [ForcAD host sizing guide](../../docs/OPERATIONS.md#sizing-the-forcad-host)
to distinguish the 10-team rehearsal estimate from the 22-team championship
deployment. [Resource limits](../../docs/OPERATIONS.md#resource-limits) lists
the Compose settings to adjust for your host.

## Install and test

The top-level `install.sh` takes no options. It installs prerequisites, clones
the configured revision into the unused install path, copies generated config
and assets, initializes the game, and starts the application. If `monitoring`
under `[env]` is `true`, it also starts Prometheus and Grafana. Scripts under
`host/` are installer helpers, not a separate procedure.

The checker files in the bundle stay private. In the installed `checkers/`
tree, each configured task's `checker` and its directories get `0755`;
supporting files get `0644`. The worker mounts that tree read-only.

The installer stages network files but does not restart networking. Apply any
address or gateway changes from the host console before installation. Run the
top-level `test.sh` after install.

Prometheus and Grafana bind to localhost. The rendered `router_metrics` and
`game_metrics` settings add scrape targets but do not install exporters on the
Router; enable those with the corresponding Router install flags.
`config/runtime/admin.env` contains ForcAD admin credentials, and
`deployment/docker/.env` contains the generated Grafana password. Keep both
private. See [monitoring access](../../docs/MONITORING.md).

The managed checkout is assigned to the original `SUDO_USER`, except
service-owned database and container storage. A direct root install remains
root-owned. Reinstallation is refused; this bundle creates a fresh deployment.

For an existing managed deployment use the new archive's top-level
`sudo bash install.sh --monitoring`. It updates the shipped dashboards and
rules from the bundle, merges scrape targets, preserves existing passwords,
and configures the checker DB reader. It neither fetches a revision nor runs
game setup, changes WireGuard keys, or restarts game containers. Provisioned
files with local modifications are refused; save custom panels under a new UID.
The existing checkout must already support the monitoring Compose overlay and
checker-reader provisioning. An older, incompatible deployment is rejected;
the add-on never migrates the game schema or upgrades its binary.
If Compose fails after staging the files, fix the reported error and rerun
`--monitoring`. The staged passwords and targets are reused; a failed command
does not mean either dashboard service is ready.
If the base game started without metrics listeners, enable their overlay at
the next planned game restart; router and checker evidence need no restart.
The update never claims pre-existing Docker images. If later package purge
reports images outside the original ownership snapshot, inspect them before
removing them or use `--keep-packages`; do not prune the host.

## Removal

`uninstall.sh --confirm` permanently removes the managed checkout, database and
volumes. There is no dry-run. Package and Docker cleanup options are in the
[command reference](../COMMANDS.md#forcad).

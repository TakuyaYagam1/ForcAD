# ForcAD

Generate the bundle with the [main guide](../README.md). See [commands and
options](../COMMANDS.md#forcad).

## Host requirements

Use Debian 12 or 13. Its live address and gateway must match `settings.toml`.
The install path must be unused, and Docker must not contain an existing ForcAD
project or volumes. This installer creates a new game; it does not update an
existing deployment.

## Install and test

The top-level `install.sh` takes no options. It installs prerequisites, clones
the configured revision into the unused install path, copies generated config
and assets, initializes the game, and starts the application. If `monitoring`
under `[env]` is `true`, it also starts Prometheus and Grafana. Scripts under
`host/` are installer helpers, not a separate procedure.

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

## Removal

`uninstall.sh --confirm` permanently removes the managed checkout, database and
volumes. There is no dry-run. Package and Docker cleanup options are in the
[command reference](../COMMANDS.md#forcad).

# ForcAD

Generate the bundle with the [main guide](../README.md). See [commands and
options](../COMMANDS.md#forcad).

## Host requirements

Use Debian 12 or 13. Its live address and gateway must match `settings.toml`.
The install path must be unused, and Docker must not contain an existing ForcAD
project or volumes. This installer creates a new game; it does not update an
existing deployment.

## Install and test

The top-level `install.sh` installs prerequisites, clones the configured
revision, installs the generated config and assets, initializes the game
configuration, and starts the application and selected monitoring. Scripts
under `host/` are helpers called by the installer.

The installer stages network files but does not restart networking. Apply any
address or gateway changes from the host console before installation. Run the
top-level `test.sh` after install.

When enabled, monitoring listens on localhost. `config/runtime/admin.env`
contains admin credentials; `deployment/docker/.env` contains the Grafana
password. Keep both private. See [monitoring access](../../docs/MONITORING.md).

## Removal

`uninstall.sh --confirm` permanently removes the managed checkout, database and
volumes. There is no dry-run. Package and Docker cleanup options are in the
[command reference](../COMMANDS.md#forcad).

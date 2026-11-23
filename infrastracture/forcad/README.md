# ForcAD

Event operators deploy the ForcAD game service from this bundle on Debian 12 or
13. See the [command reference](../COMMANDS.md#forcad) for update and removal
options.

## Host requirements

The live address and gateway must match `settings.toml`. The install path must
be unused and Docker must have no `forcad` project containers, volumes or
networks. Fresh install refuses an existing checkout. It stages network files
but does not restart networking; apply address or gateway changes from the host
console first.

## Install and test

From the extracted `forcad/` directory:

```sh
sudo bash install.sh
sudo bash test.sh
```

Fresh install takes no options. It installs prerequisites, clones the configured
revision, copies generated config and assets, initializes the game and starts
the application. `[env] monitoring = true` also starts Prometheus and Grafana.

Keep checker files private. Installed checker tasks and directories use mode
`0755`; supporting files use `0644`, and the worker mounts the tree read-only.
Keep `config/runtime/admin.env` and `deployment/docker/.env` private; they
contain ForcAD and Grafana credentials. Prometheus and Grafana bind to localhost.
`router_metrics` and `game_metrics` add scrape targets but do not install Router
exporters.

For an existing managed deployment, install the monitoring add-on on both Router
and ForcAD with `sudo bash install.sh --monitoring`. It refreshes dashboards and
rules, merges scrape targets and preserves passwords; it does not fetch a
revision, run game setup, change WireGuard keys or restart game containers.
It requires a checkout with compatible monitoring and checker-reader support.
Locally modified provisioned files are refused; save custom panels under a new
UID. If Compose fails after staging, fix the error and rerun `--monitoring`;
staged passwords and targets are reused, but dashboards may not be ready yet.

## Removal

`sudo bash uninstall.sh --confirm` permanently removes the managed checkout,
database and Compose volumes; there is no dry-run. Package and Docker cleanup
is optional. Pre-existing packages are kept, and purge stops if unrelated
Docker resources make ownership unclear. See
[COMMANDS.md#forcad](../COMMANDS.md#forcad) for cleanup options.

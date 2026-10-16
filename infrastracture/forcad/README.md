# ForcAD host

This bundle installs a new ForcAD deployment on Debian 12/13. Use a clean,
dedicated host with the address and gateway configured in `settings.toml`, and
no existing ForcAD install path, Docker project or volumes. The installer is
not an updater; do not run it over an existing game.

## Configure and render

On the workstation, copy and edit the example settings, then render the bundle:

```sh
cp -n infrastracture/settings.example.toml infrastracture/settings.toml
cd infrastracture
# Edit settings.toml: network, repository revision, game, tasks, teams,
# logos, branding and monitoring.
python3 render.py
```

The renderer creates a complete `config.yml`, environment settings, logo and
checker assets, and `runtime/forcad.tar.gz`. Review the generated bundle before
transferring it. It does not change the host or clone the repository.

## Install

Copy `runtime/forcad.tar.gz` to the host, extract it into a new directory and
run there:

```sh
bash install.sh
bash test.sh
```

The installer installs Git, Docker Engine/Compose and required host tools,
clones the configured revision, copies the generated config and assets, then
starts and checks the game. It verifies the Go archive checksum. Host network
files are staged, not applied; review `host/APPLY.txt` and apply changes from
the console if needed. A failed run can leave OS packages installed by its
bootstrap.

Keep `config/runtime/admin.env` and `deployment/docker/.env` private. The first
contains the generated admin login; the second contains the Grafana password
when monitoring is enabled. Grafana and Prometheus listen on localhost. See the
[monitoring guide](../../docs/MONITORING.md) for SSH access.

## Monitoring

In `[env]` of `settings.toml`:

- `monitoring = true` starts Prometheus and Grafana.
- `router_metrics = true` adds the router exporter target. Install the router
  component with `bash install.sh --network-metrics` after preparing its files.
- `game_metrics = true` adds the Zeek target. Install Zeek and `forcad-sensor`
  on the router, then run `bash install.sh --game-metrics` there.

Enable both router options when both exporters are ready. These settings add
ForcAD scrape targets; they do not install the router components.

## Remove

```sh
bash uninstall.sh --confirm
```

This permanently deletes the managed checkout, database and Compose volumes.
It restores host files and removes packages installed by this bootstrap.
Review [UNINSTALL.txt](UNINSTALL.txt) first.

# ForcAD host

Debian 12/13 at `10.0.2.10/24`, gateway `10.0.2.1`. Merge generated
`infrastracture/runtime/forcad/interfaces` into ifupdown config from the console. Use resolvconf
or configure DNS separately; preserve management access.

Install Docker Engine with Compose v2, GNU Make and the Go version from
[backend/go.mod](../../backend/go.mod).

## Start

From the repository root on a new installation:

```sh
cp -n config/config.yml.example config/config.yml
cp -n frontend/.env.example deployment/docker/.env
```

1. Merge generated `infrastracture/runtime/forcad/teams.yml` into `config/config.yml`. Keep team
   IPs and `traffic_cidrs`; set names, services, checkers and start time. Preserve
   existing tokens and identities.
2. Merge `infrastracture/runtime/forcad/environment.env` into `deployment/docker/.env`. It binds
   nginx and the public origin to `http://10.0.2.10`. Preserve other settings.
3. Install executable checkers under `checkers/` and configure their dependencies.

```sh
./control.sh setup
./control.sh start
./control.sh status
```

Players reach `http://10.0.2.10/` through VPN. Admin credentials are in
`config/runtime/admin.env`. After initialization, editing YAML does not update
the live PostgreSQL game; use [Operations](../../docs/OPERATIONS.md).

Checkers must originate from `10.0.2.10`. Normal Docker bridge networking does
this through host NAT; extra checker hosts require router policy changes.

## Monitoring

Export the public inventory from the final team configuration:

```sh
./control.sh network-config > config/runtime/network.json
./control.sh network-rules --config config/runtime/network.json > config/runtime/network.nft
```

Install these files and a matching-architecture `backend/bin/forcad` binary using
[Router: network metrics](../router/README.md#network-metrics). For game HTTP/WS,
install the [Zeek sensor](../router/README.md#game-httpws-metrics) too. Do not copy
team tokens, admin credentials or database secrets to the router.

Verify from ForcAD:

```sh
curl --fail http://10.0.2.1:9109/metrics
curl --fail http://10.0.2.1:9911/metrics
```

Merge generated `infrastracture/runtime/forcad/routers.yml` and `game-sensors.yml`
into their matching files in `deployment/monitoring/file_sd/`. Add only installed targets; preserve existing
ones. Set a private `GRAFANA_ADMIN_PASSWORD` in the environment or deployment
`.env`, then start the full overlay:

```sh
./control.sh monitoring up -d
```

For an existing Prometheus installation, restart it when adding new jobs/rules.
Keep the monitoring overlay for later updates/scaling. See
[Monitoring](../../docs/MONITORING.md) for commands and metric coverage.

Grafana/Prometheus listen on localhost. From an admin VPN workstation:

```sh
ssh -L 3000:127.0.0.1:3000 USER@10.0.2.10
```

Open `http://localhost:3000/`; replace `USER` with the host's SSH account.

## Verify and roll back

Check participant board access, admin SSH, real checker CHECK/PUT/GET and both
router targets in Prometheus. Send requests from a known VPN peer: the vulnbox
must see its gateway IP, while game metrics identify the source team.

Before updating, save the game config, `.env`, public inventory and database
backup. Restore the matching deployment/config revision without deleting volumes.
Router inventory and rules must match; a fingerprint mismatch fails collection.

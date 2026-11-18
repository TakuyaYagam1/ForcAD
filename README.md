# ForcAD

This branch is the independent Чемпионат России deployment. Первенство России
is published from the separate `pr-final` branch. Give each division its own
Compose project, ports, runtime configuration and database.
`config/config.yml.example` contains the three PFR checkers and participating
teams, but uses placeholder TEST-NET IPs and no tokens. Replace them in the
private `config/config.yml` before game day.

![ForcAD](public/ForcAD.png)

ForcAD is a capture-the-flag attack-defense platform.

## Choose a setup

- **Manual application:** run ForcAD and its dependencies with Docker Compose on one host. Follow this guide.
- **Full infrastructure:** generate and install separate Debian Router, VPN and ForcAD hosts with the [infrastructure guide](infrastracture/README.md).
- **Local network demo:** run the [ten-team WireGuard demo](deployment/demo/README.md) on a Linux workstation.

## Manual application setup

Install Go matching [backend/go.mod](backend/go.mod), GNU Make, Docker and the
Docker Compose v2 plugin. From the repository root, create the config files:

```sh
cp -n config/config.yml.example config/config.yml
cp -n frontend/.env.example deployment/docker/.env
```

Edit `config/config.yml` with the game schedule, teams, tasks and service
settings. It seeds a new database; PostgreSQL is authoritative after
initialization. Add executable checkers under `checkers/` and list their Python
dependencies in `checkers/requirements.txt`. Start with the
[reference checker](checkers/examples/README.md).

If players will use a public hostname or reverse proxy, set `PUBLIC_ORIGIN` in
`deployment/docker/.env` to the exact browser origin, including scheme and port
when present, with no path. Admin mutations reject a different origin. See
[Configuration](docs/CONFIGURATION.md#compose-environment) for proxy settings.

Run setup and start the containers:

```sh
./control.sh setup
./control.sh start
```

`start` does not start a game session. Open the board at
<http://127.0.0.1:8080/> and the admin panel at
<http://127.0.0.1:8080/admin/>. Start a practice or final session from the
admin panel, or let the configured schedule start the final. Setup writes the
admin credentials to the private `config/runtime/admin.env` file.

Use `./control.sh compose ...` (alias `./control.sh rd ...`) for raw Compose
commands so external storage settings are included. See
[Control CLI](docs/CONTROL.md) for the full command list and
[Operations](docs/OPERATIONS.md) before updates or resets.

## Monitoring

`deployment/docker/.env` is the private, Git-ignored Compose environment for
this division. Grafana uses username `admin` and the
`GRAFANA_ADMIN_PASSWORD` in that file, not the ForcAD admin password.
`GRAFANA_CHECKER_DB_PASSWORD` is only for its PostgreSQL reader. Keep separate
credentials and host ports for the two divisions. If either password is absent
in a fresh checkout, generate a separate 32-byte hexadecimal secret for each
before starting the overlay. After setup, run:

```sh
./control.sh monitoring up -d
```

Grafana and Prometheus listen on loopback on ports 3000/9090 for this division.
The full infrastructure installer generates a new Grafana password on the
ForcAD host. See [Monitoring](docs/MONITORING.md) for SSH tunneling and how to
install Zeek on the Router; Grafana alone does not start a traffic sensor.

![Grafana game traffic dashboard](public/grafana.png)

## Documentation

| Guide | Contents |
| --- | --- |
| [Configuration](docs/CONFIGURATION.md) | Game fields, credentials and settings that change after initialization |
| [Control CLI](docs/CONTROL.md) | Operator commands and compatibility aliases |
| [Operations](docs/OPERATIONS.md) | Scaling, resource limits, updates and resets |
| [Monitoring](docs/MONITORING.md) | Application metrics, network traffic, dashboards and access |
| [Checkers](docs/CHECKERS.md) | Checker arguments, output and status contract |
| [Architecture](docs/ARCHITECTURE.md) | Process roles, data flow and backend layout |
| [Development](docs/DEVELOPMENT.md) | Toolchain, code generation, tests and benchmarks |
| [Frontend](frontend/) and [branding](frontend/BRANDING.md) | React source and public theme settings |
| [Security checks](security/README.md) | Source, dependency and image checks |

Prepare a development checkout with:

```sh
make -C backend sync
make -C frontend sync
```

See the [development guide](docs/DEVELOPMENT.md#tests) for build and test
commands.

# Development

See the [README documentation index](../README.md#documentation) for the other
project guides.

## Toolchain

Use the Go version declared in [backend/go.mod](../backend/go.mod). CI uses Node
26.8.2 and Python 3.14.8 ([test workflow](../.github/workflows/tests.yml)); pnpm
12.11.2 is declared in the [API](../api/package.json) and
[frontend](../frontend/package.json) manifests. Python is used by API code
generation, not by the backend runtime.
GNU Make is required. Docker with the Compose plugin is required for integration
and deployment checks.

## Install dependencies

From the repository root:

```sh
make -C backend sync
make -C frontend sync
```

This creates `api/.venv`, installs the version-pinned schema-generation dependency,
downloads Go modules, installs backend linters, and installs the API and frontend
pnpm lockfiles.

## Code generation

Author the API contract in `api/index.yml`, `api/routes/`, and `api/components/`.
Choose a focused generator when changing one client, or run the full generation
target before reviewing generated output:

| Command | Generated code |
| --- | --- |
| `make -C backend oapi-generate` | Go server |
| `make -C frontend oapi-generate` | Frontend API client |
| `make -C backend generate` | Go server, sqlc queries and Wire wiring |
| `make -C frontend generate` | Frontend API client |

Run both components' `generate` targets after a shared contract change.

The OpenAPI generator bundles the contract before running ogen and Orval. It
removes the temporary `api/openapi.yml` and `api/components/schemas.yml` files
when generation exits. Review generated changes under
`backend/internal/adapter/primary/http/api`,
`backend/internal/adapter/secondary/postgres/sqlc`,
`backend/internal/bootstrap/wire_gen.go`, and
`frontend/src/shared/api/generated`.

## Backend

The Go module is in `backend/`. `cmd/forcad` contains the CLI entry point;
`internal` holds domain logic, use cases, adapters, and application wiring.
Database migrations and sqlc queries live in `backend/db/`. Run focused targets
as `make -C backend build`, `make -C backend test`, `make -C backend race`, or
`make -C backend vet`.

## Frontend development

`make -C frontend dev` starts Vite. Its `/api` and `/api/events/ws` proxies use
`FORCAD_BACKEND_URL`, defaulting to `http://127.0.0.1:8080`. To run the frontend
with local demo fixtures and its in-memory API adapter, use
`pnpm --dir frontend dev:demo`; the demo login is `demo` / `demo`.
The [frontend application](../frontend/) contains its source and package
scripts. See [BRANDING.md](../frontend/BRANDING.md) for theme settings.

## Tests

From the repository root:

```sh
make -C backend lint race test-contracts
make -C frontend lint test
make -C deployment test
make -C deployment e2e
```

Backend `lint` runs GolangCI-Lint and SQLFluff. Frontend `lint` runs ESLint and
TypeScript checks for the app and browser tests.
Backend rules live in `backend/.golangci.yml` and `backend/.sqlfluff`;
SQL checks cover migrations and sqlc queries. Generated Go code is excluded.
Use `make -C backend lint-go` or `lint-sql` for a focused check, and
`make -C backend fmt` to apply Go formatting. Go tool versions are locked in
`backend/go.mod`; SQLFluff dependencies are locked in `backend/requirements.txt`
and installed into the ignored `backend/.cache/sqlfluff` environment.

Backend `test` runs Go unit tests; `race` enables the race detector and
`test-contracts` checks the shared API tooling. Frontend `test` runs Node tests.
These targets do not start containers or run browser tests.

`make -C deployment test` runs the backend integration suite against isolated
PostgreSQL, PgBouncer, Redis, and RabbitMQ containers, then builds a temporary deployment
and checks its HTTP endpoints. `make -C frontend e2e` delegates to
`make -C deployment e2e`, which also runs the Playwright flows in `frontend/e2e`.
Both deployment targets require Docker Compose.

The deployment API checks cover public reads, team/task CRUD, session login and
logout, authorization and input errors, generation conflicts, pause/resume,
batch submission and duplicate retries, WebSocket upgrades and private metrics.
Reserved team-task CRUD methods are checked for their documented 404 response.

`make -C deployment test-monitoring` also checks the Grafana dashboard, replica
discovery and N/A values after source failure. `make -C deployment test-network`
checks generated nftables rules and the exporter in a disposable container.

For local security checks and their exact coverage, see [security/README.md](../security/README.md).

## Focused benchmarks

```sh
cd backend
go test -run='^$' -bench='^BenchmarkRuntimeConcurrent$' -benchmem -count=3 ./internal/usecase/game
./integration_test/scripts/run_tests.sh -run='^$' -bench='^BenchmarkPublishBatch$' -benchtime=3x -count=3
```

The runtime benchmark uses a delayed fake repository to measure overlapping
read suppression (`repo_calls/op`). The publisher benchmark uses isolated
RabbitMQ, includes connection setup and excludes test queue setup. The runner
enables the race detector; use identical settings for comparisons. Neither
benchmark establishes production capacity. Check frontend chunks with
`make -C frontend build` and recovery flows with `make -C frontend e2e`.

## Load regression

`make -C backend test-load` runs Vegeta against an isolated production stack:
20 teams, three storage services with Python checkers, flag farms and reconnecting
WebSocket spectators. Profiles cover smoke, sustained load, spikes, endpoint
steps up to 10000 offered RPS, and a four-hour soak. Reports distinguish actual
delivery from requested load and reconcile flag responses with the database.
See [load testing](../backend/load_test/README.md) for commands, thresholds and
measurement limits. `make -C deployment test-load` runs the same suite.

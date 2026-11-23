# Development

See the [README documentation index](../README.md#documentation) for the other project guides.

## Toolchain

Use the Go version in [backend/go.mod](../backend/go.mod). CI uses Node 26.13.1, Python 3.15.2, and pnpm 12.21.0; pnpm is declared in the [API](../api/package.json) and [frontend](../frontend/package.json) manifests. Python supports code generation, test tools, and checkers. Infrastructure rendering requires Python 3.11 or later; generated host installers support Debian 12 and 13. GNU Make is required. Docker with the Compose plugin is required for integration and deployment checks.

Frontend builds and browser-test type checks use the native TypeScript 7 compiler (`typescript-compiler`). ESLint uses the TypeScript 6 JavaScript API required by `typescript-eslint`; keep these versions separate until its parser supports 7.

## Install dependencies

From the repository root:

```sh
make -C backend sync
make -C frontend sync
```

Backend sync prepares `api/.venv`, installs the pinned schema-generation tools, downloads Go modules, and installs backend linters. Frontend sync installs its code-generation tools and frozen pnpm lockfile.

## Code generation

Author the API contract in `api/index.yml`, `api/routes/`, and `api/components/`. From the repository root, run the focused target for one client or the full target before reviewing generated files:

| Command | Generated code |
| --- | --- |
| `make -C backend oapi-generate` | Go server |
| `make -C frontend oapi-generate` | Frontend API client |
| `make -C backend generate` | Go server, sqlc queries, and Wire wiring |
| `make -C frontend generate` | Frontend API client |

Run both components' `generate` targets after a shared contract change. The OpenAPI generator bundles the contract for ogen and Orval, then removes temporary `api/openapi.yml` and `api/components/schemas.yml` files. Review output under `backend/internal/adapter/primary/http/api`, `backend/internal/adapter/secondary/postgres/sqlc`, `backend/internal/bootstrap/wire_gen.go`, and `frontend/src/shared/api/generated`.

## Backend

The Go module is in `backend/`: `cmd/forcad` is the CLI entry point, `internal` contains application logic and adapters, and `db/` holds migrations and sqlc queries. From the repository root, use `make -C backend build`, `test`, `race`, or `vet` for focused checks.

## Frontend development

From the repository root, `make -C frontend dev` starts Vite. Its `/api` and `/api/v1/events/ws` proxies use `FORCAD_BACKEND_URL`, defaulting to `http://127.0.0.1:8080`. To use the in-memory API fixture, run `pnpm --dir frontend dev:demo`; its demo login is `demo` / `demo`. See the [frontend source](../frontend/) and [theme settings](../frontend/BRANDING.md).

## Tests

From the repository root:

```sh
make -C backend lint race test-contracts
make -C backend test-checker
make -C frontend lint test
make -C deployment test
make -C deployment e2e
```

Backend `lint` runs Go, SQL, and Python style checks. `test` runs Go unit tests; `race` enables the race detector; `test-contracts` checks shared API tooling. `test-checker` runs the [reference checker](../checkers/examples/README.md) through the Go runner. These backend targets do not start containers or run browser tests. See the [checker protocol](CHECKERS.md) for the worker contract.

Frontend `lint` runs ESLint and TypeScript checks for the app and browser tests; `test` runs Node tests. `make -C deployment test` runs backend integration tests against isolated PostgreSQL, PgBouncer, Redis, and NATS JetStream containers, then builds an isolated deployment and checks its HTTP API. `make -C deployment e2e` also requires Docker Compose and runs Playwright flows in `frontend/e2e`.

The isolated deployment accepts admin requests from its Docker IPv4 network for browser and API tests; Nginx is published only on host loopback. `scripts/test_nginx_image.sh` checks the admin CIDR restriction separately. Deployment API tests cover reads, CRUD, login/logout, authorization, validation, conflicts, pause/resume, submissions, WebSocket upgrades, and private metrics.

`make -C deployment test-monitoring` checks Grafana dashboards, replica discovery, and N/A values after source failure. `make -C deployment test-network` checks generated nftables rules and the exporter in a disposable container. See the [infrastructure checks](../infrastracture/COMMANDS.md#workstation) for host firewall, Zeek, and dashboard queries.

The [network demo](../deployment/demo/README.md) defaults to 20 team segments and supports 2 to 32. It is separate from Vite's in-memory demo and the backend load suite. For local security checks and their coverage, see [security/README.md](../security/README.md).

## Focused benchmarks

Run the runtime benchmark from `backend/`; the publisher benchmark starts its isolated test dependencies:

```sh
cd backend
go test -run='^$' -bench='^BenchmarkRuntimeConcurrent$' -benchmem -count=3 ./internal/usecase
./integration_test/scripts/run_tests.sh -run='^$' -bench='^BenchmarkPublishBatch$' -benchtime=3x -count=3
```

The runtime benchmark uses a delayed fake repository and reports overlapping-read suppression as `repo_calls/op`. The publisher benchmark includes NATS JetStream connection setup but excludes test stream setup. The integration runner enables the race detector. Use identical settings for comparisons; neither benchmark establishes production capacity.

## Load regression

From the repository root, `make -C backend test-load` runs Vegeta against an isolated production-like stack with synthetic teams, fixture services, Python checkers, and WebSocket observers. Profiles measure smoke, sustained load, spikes, endpoint steps up to 10,000 offered RPS, and a four-hour soak. Reports distinguish delivered traffic from requested load and reconcile flag responses with the database. `make -C deployment test-load` runs the same suite. See [load testing](../backend/load_test/README.md) for commands, thresholds, and measurement limits.

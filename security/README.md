# Security checks

Run from the repository root. The checks inspect local source, dependencies or images; they do not start a game or contact participant services.

## Scan source and dependencies

```sh
make -C deployment security
```

Use a full Git checkout, the [project's Go version](../backend/go.mod), Python 3, Make, Trivy, Gitleaks and Semgrep. Trivy and `govulncheck` may download vulnerability data. All four targets run; a finding or scanner error makes the command fail.

| Make target | What it checks |
| --- | --- |
| `security-filesystem` | [Trivy](trivy/filesystem.yml): dependencies and Dockerfiles, including development dependencies and unfixed HIGH/CRITICAL findings. |
| `security-secrets` | Tracked and non-ignored untracked files, plus history reachable from **all Git refs**. The current-file scan skips symlinks; both reports redact values. |
| `security-semgrep` | [Local rules](semgrep/rules.yml) for Go, Python, JavaScript and TypeScript in the backend, frontend and infrastructure. Excludes infrastructure tests and rendered bundles. |
| `security-go-vuln` | Known vulnerabilities reachable from Go backend packages. |

To run one check, use its target instead of `security`. `security-test` tests the Semgrep rules and scanner scripts; it does not scan the application.

## Scan container images

```sh
make -C deployment security-images                              # Build and scan all project images
make -C deployment security-images IMAGE_TARGETS='runtime worker' # Build and scan only these two
make -C deployment security-image IMAGE=forcad-nginx:local       # Scan an existing local image by ID
```

The build matrix uses Docker and pinned Trivy: `runtime`, `worker`, `nginx`, `postgres`, `pgbouncer`, `nats`, `zeek` and `demo`. It removes temporary tags and archives without publishing images. The single-image target uses local Trivy; it does not build or pull the target.

The worker includes Python; the initializer shares the runtime filesystem. The Zeek image uses the router's Debian core package; its findings are not an inventory of any deployed router. Coverage of source-built C/C++ binaries is limited. Redis, Prometheus and Grafana are external images outside this matrix. Frontend build dependencies are covered by the source scan.

## Find a report

Local reports go to `security/reports/`; built image reports and IDs go under `images/<target>/` there. Override the output and cache with `REPORTS` and `CACHE` (default cache: `security/.cache/`).

The [security workflow](../.github/workflows/security.yml) runs on the first of each month at 06:17 UTC or by manual dispatch. GitHub retains reports for 45 days. For a failed Gitleaks job, open the `security-source-reports` artifact:

- `gitleaks-files.json`: current checkout.
- `gitleaks-history.json`: historical commit and path. A match may also occur in the current-file report.

[Tests](../.github/workflows/tests.yml) run separately on pushes and pull requests to `main`. [Dependabot](../.github/dependabot.yml) groups weekly dependency updates; maintainers update inline version pins and download checksums manually.

## Deployment boundaries

- Terminate HTTPS and set HSTS at the public edge. Configure the exact `PUBLIC_ORIGIN` and trusted proxies in [Configuration](../docs/CONFIGURATION.md#compose-environment).
- Admin writes require an HttpOnly session cookie and a session-bound `X-CSRF-Token`. The frontend sends the token only to same-origin admin endpoints. Sign in again if an older session has no token.
- Nginx restricts scripts to the same origin and disallows inline scripts and `eval`; inline CSS is allowed. The public API does not fetch arbitrary URLs. Operator health/drain clients accept HTTP(S) and reject redirects.
- [Checkers](../docs/CHECKERS.md) execute trusted organizer code. Their process limits are not a sandbox for participant code.

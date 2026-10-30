# Security checks

See the [README documentation index](../README.md#documentation).

Runtime controls include administrative session authentication, bounded JSON
requests, parameterized SQL, public DTO projections, shared rate budgets and
restricted database credentials. The [OWASP Top 10](https://top10.owasp.org/2025/)
describes risks; it is not a certification that every vulnerability is excluded.

Administrative mutations require a session-bound `X-CSRF-Token` from login or
session status. The frontend keeps it in memory and sends it only to same-origin
administrative endpoints. HttpOnly cookies and Origin/Fetch Metadata checks
remain enabled. Old sessions without a CSRF token must log in again after
upgrading. See [OWASP CSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html).

Nginx supplies CSP and framing/security headers on the SPA document as well as
the API's own headers. Scripts are limited to the same origin without inline
scripts or eval; inline CSS remains permitted for React/Radix layout. Zod uses
`jitless` mode to avoid its eval capability probe. Browser tests cover the actual
login/admin flow and CSP violations. Public deployments
need HTTPS and consistent `PUBLIC_ORIGIN`/trusted-proxy settings; configure HSTS
at the public HTTPS edge.

The public API has no general user-supplied URL fetch. Operator health/drain
clients validate HTTP(S) endpoints, connect without an environment proxy and
refuse redirects. Private addresses remain valid for organizer-controlled game
networks. Checkers are trusted code, not a sandbox; see
[checker boundaries](../docs/CHECKERS.md) and
[OWASP SSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html).

Run checks from the repository root. They inspect local source, dependencies,
and images. They do not start a game or contact participant services. Trivy and
`govulncheck` may access vulnerability data. Trivy may download checks on its
first run. Image scans require Docker; the `security-image` target also needs
an existing local image.

Requirements: Go from `backend/go.mod`, Python 3, GNU Make, Docker, Trivy,
Gitleaks, and Semgrep. Run the full source scan with:

```sh
make -C deployment security
```

The target runs `security-filesystem`, `security-secrets`, `security-semgrep`,
and `security-go-vuln`. A finding or scanner error fails the target. Run one
scanner directly, for example:

```sh
make -C deployment security-semgrep
```

| Target | Scope |
| --- | --- |
| `security-filesystem` | Trivy scans dependencies and Dockerfiles for HIGH and CRITICAL vulnerabilities and misconfigurations. It includes development dependencies and unfixed findings. The custom file patterns also include `requirements-*.txt`. |
| `security-secrets` | Gitleaks scans tracked files, non-ignored untracked files, and all reachable Git history. It requires a full checkout. Tracked files stay in scope when ignored; symlinks are excluded from the current-file copy, and report values are redacted. |
| `security-semgrep` | Local rules scan `backend/internal`, `backend/config`, `backend/cmd`, `frontend/src`, and production source under `infrastracture/`. The infrastructure scan excludes `infrastracture/tests/` and rendered bundles under `infrastracture/runtime/`. Rules cover selected Python, Go, JavaScript, and TypeScript patterns, including dynamic execution, unsafe YAML or pickle loads, shell calls, disabled TLS, bare Go HTTP helpers, `document.write`, and direct `innerHTML` writes. They do not catch every way to use these APIs. |
| `security-go-vuln` | `govulncheck` checks reachable vulnerabilities in backend packages. |
| `security-images` | Builds selected project image targets and scans their final images with pinned Trivy. Defaults to `runtime`, `worker`, `nginx`, `postgres`, and `pgbouncer`. |

The `security-image` target scans an existing local image by image ID. It does
not build, pull, or publish it:

```sh
make -C deployment security-image IMAGE=forcad-nginx:local
```

Build and scan the five repository images locally with the pinned Trivy
container:

```sh
make -C deployment security-images
```

Select targets explicitly with:

```sh
make -C deployment security-images IMAGE_TARGETS='runtime worker'
```

The supported targets are `runtime`, `worker`, `nginx`, `postgres`, and
`pgbouncer`; the workflow scans all five independently. Runtime and initializer
share a filesystem, while the worker includes the Python checker runtime. Trivy
scans packages it detects in the final images, including OS and Go/Python
packages, nginx, PostgreSQL, and PgBouncer; coverage of source-built C/C++
binaries is limited. JavaScript build dependencies remain in the
`security-filesystem` scope. Redis, RabbitMQ, Prometheus, and Grafana are
externally supplied and are not built or scanned by this image matrix. The
scanner keeps local build cache, removes its temporary image tag and archive,
and does not start application containers, sign, or publish images. JSON, table
summaries, and image IDs are stored under `REPORTS/images/<target>/`. GitHub uploads report
files only, not image archives or build cache; source and image reports are
retained as workflow artifacts for 45 days.

`make -C deployment security-test` checks Semgrep positive and negative
fixtures, then runs Python unit tests for secret-scan input handling and image
scanner cleanup/error cases. It does not run the source scanners. Reports and
caches default to `security/reports/` and `security/.cache/`; set `REPORTS` and
`CACHE` to use other paths.

GitHub Actions runs the full `security` workflow monthly on the 1st at 09:17
Europe/Moscow (`17 6 1 * *`) and supports manual dispatch. This includes the
container image builds and scans. Findings or scanner errors fail the workflow.
The separate `tests` workflow runs on pushes and pull requests targeting `main`
and supports manual dispatch. Dependabot uses one cross-ecosystem group for all
six ecosystems, including major versions, every Monday at 09:00 Europe/Moscow;
its pull requests use the normal PR tests. See
[dependabot.yml](../.github/dependabot.yml). Inline script version pins and
download checksums require manual updates. See the
[Dependabot configuration reference](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference).
Trivy and Semgrep use pinned container images in
[security.yml](../.github/workflows/security.yml).

Checker integration tests are separate; see
[development tests](../docs/DEVELOPMENT.md#tests). Scanner scope and thresholds
are defined in [deployment/Makefile](../deployment/Makefile),
[Trivy config](trivy/filesystem.yml), and [Semgrep rules](semgrep/rules.yml).

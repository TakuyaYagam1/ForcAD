# Security checks

See the [README documentation index](../README.md#documentation).

Run checks from the repository root. They inspect local source, dependencies,
and images. They do not start a game or contact participant services. Trivy and
`govulncheck` may access vulnerability data. Trivy may download checks on its
first run. Image scans require Docker and an existing local image.

Requirements: Go from `backend/go.mod`, Python 3, GNU Make, Trivy, Gitleaks, and
Semgrep. Run the full source scan with:

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
| `security-semgrep` | Local rules scan `backend/internal`, `backend/config`, `backend/cmd`, and `frontend/src`. They cover selected Python, Go, JavaScript, and TypeScript patterns, including dynamic execution, unsafe YAML or pickle loads, shell calls, disabled TLS, bare Go HTTP helpers, `document.write`, and direct `innerHTML` writes. They do not catch every way to use these APIs. |
| `security-go-vuln` | `govulncheck` checks reachable vulnerabilities in backend packages. |

Image scans use an existing local image and its image ID. They do not build,
pull, or publish it:

```sh
make -C deployment security-image IMAGE=forcad-nginx:local
```

`make -C deployment security-test` checks Semgrep positive and negative
fixtures, then runs Python unit tests for secret-scan input handling and error
cases. It does not run the source scanners. Reports and caches default to
`security/reports/` and `security/.cache/`; set `REPORTS` and `CACHE` to use
other paths.

GitHub Actions runs source checks on pushes to `main`, `master`, and
`modernization`, pull requests to `main` and `master`, and manual dispatch.
Trivy and Semgrep use pinned container images in
[security.yml](../.github/workflows/security.yml).

Checker integration tests are separate; see
[development tests](../docs/DEVELOPMENT.md#tests). Scanner scope and thresholds
are defined in [deployment/Makefile](../deployment/Makefile),
[Trivy config](trivy/filesystem.yml), and [Semgrep rules](semgrep/rules.yml).

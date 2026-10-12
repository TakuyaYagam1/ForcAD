# Security checks

See the [README documentation index](../README.md#documentation).

Run checks from the repository root. They inspect local source, dependencies,
and images; they do not start a game or contact participant services. Trivy and
govulncheck may access vulnerability data, and Trivy may download checks on its
first run. Image scans also need a local Docker daemon.

Requirements are Go from `backend/go.mod`, Python 3, GNU Make, Trivy, Gitleaks,
and Semgrep. Image scans additionally need Docker and an existing local image.

```sh
make -C deployment security-test
make -C deployment security
make -C deployment security-image IMAGE=forcad-nginx:local
```

`security` runs `security-filesystem`, `security-secrets`, `security-semgrep`,
and `security-go-vuln`; scanner errors
and findings fail the target. Run an individual check with, for example,
`make -C deployment security-semgrep`.

- `security-filesystem` scans repository dependencies and Dockerfiles with Trivy.
  Custom file patterns also include `requirements-*.txt`. It reports HIGH and
  CRITICAL findings, includes development dependencies, and includes unfixed
  vulnerabilities.
- `security-secrets` uses Gitleaks on tracked files, nonignored untracked files, and all
  reachable Git history. It requires a full checkout, keeps tracked files in
  scope even when ignored, excludes symbolic links from the current-file copy,
  and redacts report values.
- `security-semgrep` runs the local rules in `security/semgrep/rules.yml` over
  `backend/internal`, `backend/config`, `backend/cmd`, and `frontend/src`. Rules
  cover Python, Go, JavaScript, and TypeScript patterns for `eval` and function
  constructors, unsafe YAML and pickle loads, shell calls, disabled TLS, bare Go
  HTTP helpers, `document.write`, and direct `innerHTML` writes. They do not
  cover every way to call these APIs.
- `security-go-vuln` runs the `govulncheck` Go tool declared in `backend/go.mod` against
  backend packages.
- `security-image` scans an existing local Docker image by its image ID. It does not
  build, pull, or publish the image.

`make -C deployment security-test` checks Semgrep positive and negative fixtures, then runs
Python unit tests for secret-scan input handling and error cases. It does not run
the source scanners. Reports and caches default to `security/reports/` and
`security/.cache/`; set `REPORTS` and `CACHE` to use other paths.

GitHub Actions runs the source checks on pushes to `main`, `master`, and
`modernization`, on pull requests to `main` and `master`, and on manual dispatch.
Trivy and Semgrep use pinned container images
in [security.yml](../.github/workflows/security.yml).

Checker integration coverage is separate from these scanner targets; see
[development tests](../docs/DEVELOPMENT.md#tests).

Scanner scope and thresholds are defined in [deployment/Makefile](../deployment/Makefile),
[Trivy config](trivy/filesystem.yml), and [Semgrep rules](semgrep/rules.yml).

# Security checks

Local checks for ForcAD source code, dependencies, and built images. These commands
do not start a game, reset databases, or contact participant services.

Requirements: Python 3.11+, GNU Make, Trivy, Gitleaks, and Semgrep in `PATH`.
Install the tools from their official distributions; this directory does not
provide an installer. Image scans also require a local Docker daemon. Trivy
downloads its vulnerability database and checks, so the first run needs network
access.

Run from the repository root:

```sh
make -C security test
make -C security check
make -C security filesystem
make -C security secrets
make -C security semgrep
make -C security image IMAGE=forcad-nginx:local
```

`check` runs all three source checks, even when one fails. Findings and scanner
errors both produce a nonzero exit status. Trivy blocks HIGH/CRITICAL findings,
including vulnerabilities without a published fix and development dependencies.
Severity determines review priority; it does not prove that every vulnerability
is reachable in this application.

`secrets` scans current tracked files and nonignored untracked files, then all
reachable Git history. Tracked files remain in scope even if they match
`.gitignore`. Ignored local configuration and symbolic links are excluded from
the current snapshot. Use a full checkout, with `fetch-depth: 0` in CI. Reports
redact secret values. Exceptions cover only two public fixture tokens in specific
test files.

Semgrep uses local rules without downloading a registry ruleset. Nine rules cover
dynamic code execution, unsafe deserialization, shell calls, disabled TLS
verification, and unsafe HTML rendering. Coverage is limited to the listed APIs;
arbitrary import aliases and other ways to call the same functions may go
undetected. The `test` target checks rule fixtures without executing them as
application code.

`image` scans an existing local Docker image by its immutable ID. It does not
build, pull, or publish images. Scan every image that will be deployed, including
storage services and workers: source scans do not cover OS packages in images.

JSON reports and tool versions are written to the ignored `security/reports/`
directory. Caches use `security/.cache/`. Override these paths when needed:

```sh
make -C security check REPORTS=/private/reports CACHE=/private/cache
```

GitHub Actions runs these source checks through `.github/workflows/security.yml`
for `main`, `master`, and `fsp-prod`, using pinned tool versions. Do not use
`continue-on-error` or broad exclusions to hide findings. `trivy/ignore.yml` starts
empty; any future exception must identify the package, path, reason, owner, and
expiry date.

These checks do not verify scoring, CHECK/PUT/GET ordering, pause behavior,
concurrent flag submissions, or backup completeness. Those require separate
tests and a rehearsal on the target infrastructure.

Checker jobs persist their recovery deadline in PostgreSQL. Publication and each
CHECK/PUT/GET start allow the checker timeout plus five minutes for further
progress. Ticker checks expired jobs every five seconds, including during a game
pause. Recovery records CHECK_FAILED without changing SLA counters or scores;
it does not replay PUTs. Late results and flag inserts from closed jobs are
ignored. This prevents a lost delivery or callback from blocking rounds forever,
but does not recover the missing verdict or replace capacity testing. For an
existing database, run `backend/scripts/apply_fixes.py` in the backend environment
before starting the updated ticker and workers. New installations initialize the
schema automatically.

Documentation: [Trivy](https://trivy.dev/docs/latest/),
[Gitleaks](https://github.com/gitleaks/gitleaks),
[Semgrep](https://semgrep.dev/docs/running-rules).

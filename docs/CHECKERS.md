# Checkers

[Documentation index](../README.md#documentation)

A checker is an executable program. The Go worker starts it as a subprocess;
Python checkers can keep `#!/usr/bin/env python3`. Put programs in `checkers/`,
make them executable, and declare dependencies in `checkers/requirements.txt`.
The worker mounts the directory at `/checkers` and uses `/opt/checkers/bin` for
Python dependencies. Set each task's relative `checker` path in
[configuration](CONFIGURATION.md).

The worker uses Python 3.14 on Alpine Linux (musl). Build native checker binaries
for musl or link them statically; existing glibc binaries need a compatible custom
worker image. Python extensions are installed inside the worker build, so do not
copy a host virtual environment into `/opt/checkers`.

Start from the [reference example](../checkers/examples/README.md): a notes
checker with ordinary or PFR output, bounded HTTP requests and integration tests
through the Go runner. Copy its three files and adapt the service operations.

## Arguments

The worker passes separate arguments without a shell:

```text
checker check HOST
checker put HOST PRIVATE_DATA FLAG PLACE
checker get HOST PRIVATE_DATA FLAG PLACE
```

For PUT, `PRIVATE_DATA` starts as a random 40-character hexadecimal identifier.
`FLAG` is 31 uppercase ASCII letters/digits followed by `=`. `PLACE` is a random
integer from 1 through the task's `places`. GET receives the saved private data,
flag and location for a previously stored flag.

## Output and exit codes

For CHECK, GET and failed PUT, stdout is the public status message and stderr is
private diagnostic text. Do not put credentials or flag values in public messages.

A successful PUT has a different output contract:

| `checker_type` | stdout | stderr | Data passed to GET |
| --- | --- | --- | --- |
| `hackerdom` (default) | Private retrieval data | Diagnostics | Trimmed stdout |
| `pfr` | Public retrieval hint | Private retrieval data | Trimmed stderr |
| `nfr` | Ignored for retrieval | Diagnostics | Original PUT identifier |

Tags are separated with underscores. If both `nfr` and `pfr` are present,
`nfr` takes precedence for stored data. Public hints for `pfr` tasks are exposed
through `GET /api/client/attack_data/`. Keep successful PUT output free of debug
lines that would become retrieval data.

| Exit code | Meaning |
| --- | --- |
| `101` | UP: the action succeeded |
| `102` | CORRUPT: a stored flag is absent or altered |
| `103` | MUMBLE: the service returned invalid behavior/data |
| `104` | DOWN: the service is unavailable |
| `110` | CHECK_FAILED: the checker or platform could not complete the check |

Exit code `0` is not success in this protocol. Unsupported codes become
`CHECK_FAILED`. A checker action exceeding `checker_timeout` becomes `DOWN`.
A completed workflow contributes one SLA check even when it ends as
`CHECK_FAILED`; only `UP` counts as passed. Scheduler recovery of expired jobs is
different: it records `CHECK_FAILED` without changing SLA counters.

## Execution and limits

In classic mode CHECK runs first. If it succeeds, the PUT and GET branches can
run concurrently. PUTs can run concurrently; GETs run sequentially and stop on a
failed GET. In blitz mode, PUT-only and CHECK/GET workflows are scheduled
separately. Checkers must not rely on a shared process or in-memory state between
actions. Missing flags in a selected GET round skip that retrieval.

The default worker allows 8 concurrent checker processes, 1 MiB combined output
per action, and a 1024 MiB address-space limit per process on Linux. The task's
per-action timeout and the worker's total workflow deadline are separate limits.
On Linux, timed-out processes receive termination followed by a forced kill of
the process group if needed. These limits do not make a checker a security sandbox.

See [Operations](OPERATIONS.md) for deployment settings. The implementation is in
the [process runner](../backend/internal/adapter/secondary/checker/checker.go) and
[workflow worker](../backend/internal/worker/checker/worker.go).

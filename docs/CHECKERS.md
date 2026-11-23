# Checkers

[Documentation index](../README.md#documentation)

Use this guide to add an executable checker for a service. Put it under `checkers/`, make it executable, declare dependencies in `checkers/requirements.txt`, and set the task's relative `checker` path in [configuration](CONFIGURATION.md). The worker mounts checkers at `/checkers` and Python dependencies at `/opt/checkers/bin`.

The worker uses Python 3.15 on Alpine Linux with musl. Build native checker binaries for musl or link them statically; glibc binaries need a compatible custom worker image. Python extensions are installed in the worker image. Do not copy a host virtual environment into `/opt/checkers`.

Start from the [reference example](../checkers/examples/README.md), which shows ordinary and PFR output, bounded HTTP calls, and tests through the Go runner.

## Arguments

The worker starts each action as a separate process and passes arguments without a shell:

```text
checker check HOST
checker put HOST PRIVATE_DATA FLAG PLACE
checker get HOST PRIVATE_DATA FLAG PLACE
```

For PUT, `PRIVATE_DATA` starts as a random 40-character lowercase hexadecimal identifier. `FLAG` is 31 uppercase ASCII letters or digits followed by `=`; its first character is the uppercase first character of the task name, so task names must start with an ASCII letter or digit. `PLACE` is a random integer from 1 through the task's `places`. GET receives the saved private data, flag, and place for a previously stored flag.

## Output and exit codes

For CHECK, GET, and failed PUT, stdout is the public status message and stderr is private diagnostic text. Never put credentials or flag values in public messages.

A successful PUT stores output according to `checker_type`:

| Type | stdout | stderr | Data passed to GET |
| --- | --- | --- | --- |
| `hackerdom` (default) | Private retrieval data | Diagnostics | Trimmed stdout |
| `pfr` | Public retrieval hint | Private retrieval data | Trimmed stderr |
| `nfr` | Ignored for retrieval | Diagnostics | Original PUT identifier |

Checker tags are separated with underscores. If both `nfr` and `pfr` are present, `nfr` takes precedence for stored data. PFR hints are exposed through `GET /api/v1/client/attack_data/`. Keep successful PUT output free of debug lines because ForcAD stores it as retrieval data.

| Exit code | Meaning |
| --- | --- |
| `101` | UP: action succeeded |
| `102` | CORRUPT: stored flag is absent or altered |
| `103` | MUMBLE: invalid service behavior or data |
| `104` | DOWN: service is unavailable |
| `110` | CHECK_FAILED: checker or platform could not complete the check |

Exit code `0` is not success. Unsupported codes become `CHECK_FAILED`; an action exceeding `checker_timeout` becomes `DOWN`. With `extra_checks = 0`, each completed workflow contributes one SLA check even if it ends as `CHECK_FAILED`; only UP counts as passed. Recovery of an expired job records `CHECK_FAILED` without changing SLA counters.

With experimental `game.extra_checks > 0`, the main classic workflow and additional CHECK-only jobs form one round observation. A confirmed service failure fails the observation. If there is no confirmed service failure but a job returns `CHECK_FAILED`, the observation is `CHECK_FAILED` and SLA counters do not change. Extra jobs never run PUT or GET, so CHECK must support concurrent calls to the same service.

## API path aliases

For compatibility, NGINX rewrites these paths internally:

- `GET /attack_data`, `/attack_data/`, `/api/attack_data`, `/api/attack_data/`, `/api/v1/attack_data`, and `/api/v1/attack_data/` route to `/api/v1/client/attack_data/`.
- `PUT /api/flags`, `/api/flags/`, `/api/v1/flags`, and `/api/v1/flags/` route to `/flags/`.

Rewrites preserve the method, body, and query string.

## Execution and limits

In classic mode, CHECK runs first. If it succeeds, PUT and GET branches can run concurrently. PUTs can run concurrently; GETs run sequentially and stop at the first failed GET. Blitz mode schedules PUT-only and CHECK/GET workflows separately. Actions cannot rely on shared process or in-memory state. A selected GET round with no flags skips retrieval.

The admin scoreboard shows CHECK, PUT, and GET outcomes separately for the selected team's service and current round. For example, `PUT 5/5` means five recorded results out of five scheduled PUTs for that service, regardless of team count. Before round 1, the panel shows configuration limits rather than completed work.

The default worker allows eight concurrent checker processes, 1 MiB combined output per action, and a 1024 MiB address-space limit per process on Linux. The per-action timeout and total workflow deadline are separate. On Linux, timed-out processes receive termination followed by a forced process-group kill if needed. These limits do not make a checker a security sandbox.

Install only trusted organizer checker code and dependencies. A checker runs as the worker's operating-system identity and can access files and network destinations available to that identity. Never run participant-supplied programs in a worker. Supporting untrusted programs requires a separate execution identity, credential-free mounts, and restricted network access.

Team addresses are organizer-managed literal IPv4 or IPv6 addresses; private game networks intentionally remain valid. Keep requests on the assigned service endpoint, disable redirects and proxies unless explicitly needed, and bound each request and the whole action. The runner cannot enforce HTTP or DNS behavior in arbitrary checker code; stronger restrictions require deployment network isolation.

The runner removes reserved platform connection variables (`DATABASE_*`, `POSTGRES_*`, `PGBOUNCER_*`, `NATS_*`, `FORCAD_RUNTIME_DB_*`, NATS and Redis URLs and passwords), admin credentials, the control token, configuration path, and libpq credential-file settings from the child environment. OS settings and checker-specific variables remain available; `env_path` prepends the executable search path. This reduces accidental secret propagation but does not prevent a trusted checker from accessing other resources available to its worker identity.

Successful PUT stdout is stored according to the checker protocol, not used as a status message. For other actions, the board verdict and history use the same public text: the worker masks flag-shaped strings and the exact flag and private data passed to that action. It cannot identify arbitrary credentials, fragments, or encoded values; send those only to stderr.

See [Operations](OPERATIONS.md) for deployment settings. The implementation is in the [process runner](../backend/internal/adapter/secondary/checker/checker.go) and [workflow worker](../backend/internal/worker/checker/worker.go).

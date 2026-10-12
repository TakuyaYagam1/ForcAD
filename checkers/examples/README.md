# Checker examples

Copy the complete `note/` directory into `checkers/<service>/` and adapt its API
calls to your service. The example uses Python's standard library; no additional
dependencies are needed. A matching notes server is a test fixture, not a
competition service included in the deployment.

| File | Responsibility |
| --- | --- |
| [checker.py](note/checker.py) | CHECK/PUT/GET scenarios and output channels |
| [client.py](note/client.py) | HTTP requests, deadlines, response validation |
| [protocol.py](note/protocol.py) | CLI arguments, verdicts, versioned private data |

## Service contract

The example expects HTTP on port `8080`. Manual runs also accept `HOST:PORT` or
`[IPv6]:PORT`. ForcAD normally supplies a team IP; change `DEFAULT_PORT` for your
service. Every request uses `Authorization: Bearer <token>`. A new note receives
a random read token; the token is private retrieval data, never a public hint.

| Request | Success response |
| --- | --- |
| `PUT /notes/{id}` with JSON `{"title":"...","body":"..."}` | `201`, JSON `{"id":"..."}` |
| `GET /notes/{id}` | `200`, JSON `{"id":"...","title":"...","body":"..."}` |
| `DELETE /notes/{id}` | `204`, empty body |

IDs contain 40 lowercase hex characters. For PUT the ID comes from ForcAD.
All successful responses except DELETE use `Content-Type: application/json`.
Place `1` stores the flag in `title`; place `2` stores it in `body`.
The service must preserve notes and credentials for the configured flag lifetime.

## Adaptation

1. Replace the endpoints and scenarios with real user operations. CHECK creates,
   reads and deletes its own random note. It must exercise useful behavior beyond
   a health endpoint. Failed checks can leave a smoke note; service retention
   should clean up old records.
2. Keep PUT's readback before returning UP. GET retrieves the existing record,
   compares the selected field exactly and never recreates or deletes it.
3. Set `places` to the implemented locations. Adjust timeouts together:
   requests use 2 seconds, the client checks an 8-second action budget between
   operations/body reads, and ForcAD enforces `checker_timeout: 10` for the whole
   subprocess, including DNS and response headers.
4. Preserve versioned retrieval data. CHECK, PUT and GET are separate processes
   and may run on different workers. Do not use local files or global mutable
   state to remember credentials; do not derive secrets from flag contents.
5. Keep status messages fixed and safe. Never print flags, credentials, raw
   responses or exception tracebacks. Successful PUT is the sole exception:
   its designated private output channel contains retrieval credentials.

Default output uses `PUBLIC_HINT = False` and `checker_type: hackerdom`.
For public attack hints, set `PUBLIC_HINT = True` in your copied `checker.py`
and `checker_type: pfr` in the game config. PUT then writes only the note ID to
stdout and private JSON to stderr. Do not combine debug output with either.
See the complete [checker protocol](../../docs/CHECKERS.md).

For this API, missing/changed stored flags and rejected saved credentials during
GET produce CORRUPT. Invalid JSON, schema or behavior produce MUMBLE. Connection
errors, timeouts, HTTP 429 and HTTP 5xx produce DOWN. Invalid checker inputs and
unexpected programming errors produce CHECK_FAILED. Adapt HTTP status meanings
to your service rather than treating every unsuccessful request as CORRUPT.
The client bounds response size and does not follow redirects or retry writes.

## Run and verify

After copying the example, configure `checker: <service>/checker.py`,
`checker_type: hackerdom`, `places: 2` and `checker_timeout: 10`. Preserve the
executable bit with `chmod +x checkers/<service>/checker.py`.

```sh
./checkers/examples/note/checker.py check 127.0.0.1:8080
```

Success exits with `101`, not `0`. For a manual PUT, supply a random 40-character
hex ID, a flag of 31 uppercase letters/digits followed by `=`, and place `1` or
`2`. Pass the saved private output unchanged to GET with the same flag and place.

From the repository root, test both output modes against a local HTTP fixture:

```sh
make -C backend test-checker
```

The tests execute Python through the real Go runner and cover both places,
independent GET processes, corrupted/missing flags, bad responses, timeouts,
invalid private state and output privacy. They require Go and Python 3.10+;
no Docker, database or game network is used. Tests live in
[backend/integration_test/checker](../../backend/integration_test/checker/).

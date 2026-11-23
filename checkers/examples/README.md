# Checker examples

Use this Python standard-library example to add a service checker. Copy the complete [note directory](note/) to `checkers/<service>/`, then adapt its service operations. The matching notes fixture is available in the [local network demo](../../deployment/demo/README.md); replace it for a competition.

| File | Purpose |
| --- | --- |
| [checker.py](note/checker.py) | CHECK, PUT, and GET operations and output channels |
| [client.py](note/client.py) | HTTP requests, deadlines, and response validation |
| [protocol.py](note/protocol.py) | Arguments, verdicts, and versioned private data |

## Service contract

The example uses port `8080` by default. ForcAD supplies the team IP; change `DEFAULT_PORT` in `client.py` for another port. Manual runs also accept `HOST:PORT` and `[IPv6]:PORT`. Requests use `Authorization: Bearer <token>`. A new note gets a random read token, which must remain in private retrieval data.

| Request | Success response |
| --- | --- |
| `PUT /notes/{id}` with `{"title":"...","body":"..."}` | `201` with `{"id":"..."}` |
| `GET /notes/{id}` | `200` with the note ID, title, and body |
| `DELETE /notes/{id}` | `204` with an empty body |

Note IDs are 40 lowercase hexadecimal characters. For PUT, ForcAD supplies the ID. Successful responses other than DELETE use `Content-Type: application/json`. Place 1 stores the flag in `title`; place 2 stores it in `body`. Keep notes and credentials available for the configured flag lifetime.

## Adapt the checker

1. Replace the endpoints and scenarios with real service operations. CHECK should create, read, and delete its own random record, and test useful behavior rather than only a health endpoint. A failed CHECK can leave a smoke record, so remove old records.
2. After PUT, read the record back before returning UP. GET must retrieve the existing record, compare the selected field exactly, and leave it unchanged.
3. Set `places` to the number of implemented flag locations. Each request has a 2-second timeout and one checker action has an 8-second budget. ForcAD's `checker_timeout: 10` limits the whole subprocess, including DNS and response headers.
4. Keep versioned retrieval data because CHECK, PUT, and GET run as separate processes and may use different workers. Do not rely on local files or mutable global state for credentials, and do not derive secrets from flags.
5. Keep status messages fixed and safe. Do not print flags, credentials, raw responses, or tracebacks. Successful PUT writes retrieval data only to its designated private output channel.

The example defaults to `PUBLIC_HINT = False` and `checker_type: hackerdom`. For PFR mode, set `PUBLIC_HINT = True` and `checker_type: pfr`: PUT prints only the note ID to stdout and writes private JSON to stderr. Keep debug output out of both channels. See the [checker protocol](../../docs/CHECKERS.md).

## Verdicts

| Condition | Verdict |
| --- | --- |
| A stored flag changed or is missing, or GET rejects saved credentials | CORRUPT |
| Invalid JSON, schema, or service behavior | MUMBLE |
| Connection error, timeout, HTTP 429, or HTTP 5xx | DOWN |
| Invalid checker input or unexpected programming error | CHECK_FAILED |

Adapt HTTP status handling to the service. Do not classify every unsuccessful request as CORRUPT. The example caps response size and does not follow redirects or retry writes.

## Run and verify

From the repository root, after copying the example to `checkers/myservice/`, set `checker: myservice/checker.py`, `checker_type: hackerdom`, `places: 2`, and `checker_timeout: 10`. Give the registered entrypoint mode `0755`; leave `client.py` and `protocol.py` non-executable.

```sh
chmod 0755 checkers/myservice/checker.py
./checkers/myservice/checker.py check 127.0.0.1:8080
make -C backend test-checker
```

Successful CHECK exits with status `101`, not `0`. The test target runs Python through ForcAD's Go checker runner and uses a local HTTP fixture. It covers both output modes and places, separate GET processes, altered or missing flags, invalid responses and private state, timeouts, and output privacy. Tests require Go and Python 3.10 or later; they do not use Docker, a database, or the game network. See the [tests](../../backend/integration_test/checker/).

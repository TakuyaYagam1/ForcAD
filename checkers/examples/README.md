# Checker examples

Copy the complete `note/` directory into `checkers/<service>/` and adapt its
API calls and scenarios to your service. The example uses Python's standard
library. A matching notes fixture runs in the [local network demo](../../deployment/demo/README.md);
replace it for a competition.

| File | Responsibility |
| --- | --- |
| [checker.py](note/checker.py) | CHECK, PUT, and GET scenarios; output channels |
| [client.py](note/client.py) | HTTP requests, deadlines, and response validation |
| [protocol.py](note/protocol.py) | CLI arguments, verdicts, and versioned private data |

## Service contract

The example defaults to HTTP port `8080`. ForcAD supplies the team IP; change
`DEFAULT_PORT` in `client.py` if the service uses another port. Manual runs
also accept `HOST:PORT` and `[IPv6]:PORT`. Every request uses
`Authorization: Bearer <token>`. A new note gets a random read token; keep it
in private retrieval data, never in a public hint.

| Request | Success response |
| --- | --- |
| `PUT /notes/{id}` with JSON `{"title":"...","body":"..."}` | `201` with JSON `{"id":"..."}` |
| `GET /notes/{id}` | `200` with JSON `{"id":"...","title":"...","body":"..."}` |
| `DELETE /notes/{id}` | `204` with an empty body |

Note IDs contain 40 lowercase hex characters. For PUT, ForcAD supplies the ID.
All successful responses except DELETE use `Content-Type: application/json`.
Place 1 stores the flag in `title`; place 2 stores it in `body`. The service
must keep notes and credentials available for the configured flag lifetime.

## Adapt the checker

1. Replace the endpoints and scenarios with real user operations. CHECK creates,
   reads, and deletes its own random note. It must test useful service behavior,
   not only a health endpoint. A failed check can leave a smoke note, so the
   service should remove old records.
2. After PUT, read the note back before returning UP. GET must retrieve the
   existing note, compare the selected field exactly, and leave it unchanged.
3. Set `places` to the number of implemented flag locations. The client uses a
   2-second timeout per request and an 8-second budget for one checker action.
   ForcAD's `checker_timeout: 10` bounds the whole subprocess, including DNS and
   response headers.
4. Preserve versioned retrieval data. CHECK, PUT, and GET run as separate
   processes and may use different workers. Do not rely on local files or
   global mutable state for credentials, and do not derive secrets from flags.
5. Keep status messages fixed and safe. Do not print flags, credentials, raw
   responses, or tracebacks. Successful PUT writes retrieval credentials only
   to its designated private output channel.

By default, `PUBLIC_HINT = False` and `checker_type: hackerdom`. For public
attack hints, set `PUBLIC_HINT = True` in your copy and `checker_type: pfr` in
the game config. In PFR mode, PUT prints only the note ID to stdout and writes
private JSON to stderr. Keep debug output out of both channels. See the
[checker protocol](../../docs/CHECKERS.md).

## Verdicts

| Condition | Verdict |
| --- | --- |
| A stored flag changed or is missing, or GET rejects saved credentials | CORRUPT |
| Invalid JSON, schema, or service behavior | MUMBLE |
| Connection error, timeout, HTTP 429, or HTTP 5xx | DOWN |
| Invalid checker input or unexpected programming error | CHECK_FAILED |

Adapt HTTP status handling to your service. Do not classify every unsuccessful
request as CORRUPT. The client caps response size and does not follow redirects
or retry writes.

## Run and verify

After copying the example to `checkers/myservice/`, set
`checker: myservice/checker.py`, `checker_type: hackerdom`, `places: 2`, and
`checker_timeout: 10`. Preserve the executable bit:

```sh
chmod +x checkers/myservice/checker.py
./checkers/myservice/checker.py check 127.0.0.1:8080
```

Success exits with status `101`, not `0`. For a manual PUT, use a random
40-character lowercase hex ID, a flag made of 31 uppercase letters or digits
followed by `=`, and place `1` or `2`. Pass the saved private output unchanged
to GET with the same flag and place.

From the repository root, run the example tests against a local HTTP fixture:

```sh
make -C backend test-checker
```

The tests run Python through ForcAD's Go checker runner. They cover both output
modes, both places, separate GET processes, missing or changed flags, invalid
responses, timeouts, invalid private state, and output privacy. They require Go
and Python 3.10+. They do not use Docker, a database, or the game network. Test
code is in [backend/integration_test/checker](../../backend/integration_test/checker/).

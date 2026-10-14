import json
import secrets
import sys
import time
import urllib.error
import urllib.request


def request(base, path, body=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = token
    req = urllib.request.Request(
        base + path,
        data=json.dumps(body).encode() if body is not None else None,
        headers=headers,
    )
    try:
        with urllib.request.urlopen(req, timeout=2) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as exc:
        return exc.code, json.load(exc)


def put(service, base, flag):
    state = {}
    if service == "vault":
        state = {"name": secrets.token_hex(12), "password": secrets.token_hex(16)}
        assert request(base, "register", state)[0] == 201
        status, login = request(base, "login", state)
        assert status == 200
        state["token"] = login["token"]
    path = {"notes": "notes", "vault": "records", "queue": "jobs"}[service]
    status, saved = request(base, path, {"body": flag}, state.get("token"))
    assert status == (202 if service == "queue" else 201)
    state["path"] = path + "/" + saved["id"]
    return state


def get(service, base, state):
    if service == "vault":
        status, login = request(base, "login", state)
        assert status == 200
        state["token"] = login["token"]
    for _ in range(20):
        status, result = request(base, state["path"], token=state.get("token"))
        if status == 200:
            return result["body"]
        if status != 202:
            raise AssertionError("stored record missing")
        time.sleep(0.1)
    raise TimeoutError("job did not finish")


def main(service):
    action, host = sys.argv[1:3]
    base = f"http://{service}:8080/{host}/"
    try:
        if action == "check":
            value = secrets.token_hex(16)
            assert get(service, base, put(service, base, value)) == value
            print("service smoke passed")
        elif action == "put":
            state = put(service, base, sys.argv[4])
            assert get(service, base, state) == sys.argv[4]
            print(json.dumps(state, separators=(",", ":")))
        elif action == "get":
            assert get(service, base, json.loads(sys.argv[3])) == sys.argv[4]
        else:
            raise ValueError("unknown checker action")
        return 101
    except AssertionError as exc:
        print(str(exc) or "stored flag mismatch")
        return 102
    except (OSError, TimeoutError) as exc:
        print("service unavailable")
        print(type(exc).__name__, file=sys.stderr)
        return 104
    except (ValueError, KeyError, TypeError) as exc:
        print("invalid service behavior")
        print(type(exc).__name__, file=sys.stderr)
        return 103

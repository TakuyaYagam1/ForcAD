#!/usr/bin/env python3
"""Check APIs and state through a synthetic production Compose deployment."""
import base64
import http.client
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import time
import uuid
import urllib.error
import urllib.parse
import urllib.request

from check_api import check_admin, check_flag_input, check_public, check_unauthorized, expect

root, project, origin = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
if not project.startswith("forcad-deployment-") or not root.name.startswith("forcad-deployment."):
    raise SystemExit("Expected an isolated deployment fixture")
compose = [
    "docker", "compose", "--project-name", project,
    "--project-directory", str(root / "deployment/docker"),
    "-f", str(root / "deployment/docker/compose.yml"),
    "-f", str(Path(__file__).resolve().parents[3] / "deployment/docker/compose.e2e.yml"),
]
runtime_env_setting = os.environ.get("FORCAD_E2E_ADMIN_ENV_FILE")
runtime_env = Path(runtime_env_setting) if runtime_env_setting else root / "runtime.env"
if not runtime_env_setting and not runtime_env.exists():
    runtime_env = root / "config/runtime/admin.env"
if runtime_env.stat().st_mode & 0o077:
    raise SystemExit("Runtime credentials must be private")
credentials = {
    key: value
    for line in runtime_env.read_text(encoding="utf-8").splitlines()
    if line and not line.startswith("#")
    for key, value in [line.split("=", 1)]
}
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def request(path, data=None, method="GET", token=None, extra_headers=None):
    headers = {"Content-Type": "application/json", "Origin": origin}
    if token:
        headers["X-Team-Token"] = token
    headers.update(extra_headers or {})
    req = urllib.request.Request(
        origin + path,
        data=json.dumps(data).encode() if data is not None else None,
        method=method,
        headers=headers,
    )
    try:
        response = client.open(req, timeout=5)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        return response.status, response.read(1 << 20)


def query(sql):
    result = subprocess.run(
        compose + [
            "exec", "-T", "postgres", "sh", "-ec",
            'exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1',
        ],
        input=sql,
        text=True,
        capture_output=True,
        timeout=5,
        check=True,
    )
    return result.stdout.strip()


def current_generation():
    return int(query("SELECT generation FROM GameSession WHERE id=1;"))


def sql_literal(value):
    return "'" + value.replace("'", "''") + "'"


team_two_id = query(
    "SELECT id FROM Teams WHERE token="
    + sql_literal(credentials["FORCAD_E2E_TEAM_TWO_TOKEN"])
    + ";"
)
try:
    parsed_team_two_id = uuid.UUID(team_two_id)
except ValueError as exc:
    raise SystemExit("Could not resolve the configured second team UUID") from exc
if str(parsed_team_two_id) != team_two_id:
    raise SystemExit("The second team UUID from the database is not canonical")
existing_flag_ids = set(query("SELECT id FROM Flags;").splitlines())


assert request("/")[0] == 200, "frontend unavailable"
check_public(request, team_two_id)
check_unauthorized(request, team_two_id)
expect(request, "/api/admin/login/", 403, {"username": "invalid", "password": "invalid"}, "POST")
status, _ = request(
    "/api/admin/login/",
    {"username": credentials["ADMIN_USERNAME"], "password": credentials["ADMIN_PASSWORD"]},
    "POST",
)
assert status == 200, "admin login failed"
check_admin(request)

# The optional browser flow runs first and returns the game to waiting. Read the
# current generation from persisted state instead of assuming prior transitions.
status, _ = request(
    "/api/admin/game/start_final/",
    {"confirm": True, "generation": current_generation()},
    "POST",
)
assert status == 200, "final did not start"
check_flag_input(request, credentials["FORCAD_E2E_TEAM_ONE_TOKEN"])
expect(request, "/api/admin/game/pause/", data={}, method="POST")
expect(request, "/flags", 503, [], "PUT", token=credentials["FORCAD_E2E_TEAM_ONE_TOKEN"])
expect(request, "/api/admin/game/resume/", data={}, method="POST")

flag = ""
for _ in range(40):
    new_flags = [
        row.split("|", 1)
        for row in query(
            f"SELECT id,flag FROM Flags WHERE team_id={sql_literal(team_two_id)}::uuid;"
        ).splitlines()
        if row.split("|", 1)[0] not in existing_flag_ids
    ]
    if new_flags:
        flag = new_flags[0][1]
        break
    time.sleep(0.25)
assert len(flag) == 32, "Python checker did not persist a flag"
status, body = request(
    "/flags", [flag] * 100, "PUT", credentials["FORCAD_E2E_TEAM_ONE_TOKEN"]
)
assert status == 200 and body.count(b"Flag accepted!") == 1 and body.count(b"Flag already stolen") == 99, "batch verdicts incorrect"
assert query("SELECT count(*) FROM StolenFlags;") == "1", "duplicate scoring"
expect(request, "/flags/", data=[flag], method="PUT", token=credentials["FORCAD_E2E_TEAM_ONE_TOKEN"])
assert query("SELECT count(*) FROM StolenFlags;") == "1", "retry scored a flag twice"

address = urllib.parse.urlsplit(origin)
connection = http.client.HTTPConnection(address.hostname, address.port, timeout=5)
connection.request(
    "GET", "/api/events/ws",
    headers={
        "Connection": "Upgrade",
        "Upgrade": "websocket",
        "Sec-WebSocket-Version": "13",
        "Sec-WebSocket-Key": base64.b64encode(secrets.token_bytes(16)).decode(),
        "Origin": origin,
    },
)
response = connection.getresponse()
assert response.status == 101, "native WebSocket upgrade failed through nginx"
connection.close()

status, _ = request(
    "/api/admin/game/finish/",
    {"confirm": True, "generation": current_generation()},
    "POST",
)
assert status == 200, "final did not finish"
assert request("/flags", [flag], "PUT", credentials["FORCAD_E2E_TEAM_ONE_TOKEN"])[0] == 410, "finished game accepts flags"
expect(request, "/api/admin/logout/", data={}, method="POST")
expect(request, "/api/admin/status/", 403)
print("Deployment checks passed: public reads, admin CRUD/auth/validation, lifecycle, batch/retry, Python checker, WebSocket, and private metrics.")

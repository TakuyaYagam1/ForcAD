#!/usr/bin/env python3
"""Check APIs and state through a synthetic production Compose deployment."""

import base64
import http.client
import http.cookiejar
import json
import os
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

from check_api import (
    check_admin,
    check_flag_input,
    check_public,
    check_unauthorized,
    expect,
)
from fixture_permissions import require_private_file

root, project, origin = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
if not project.startswith("forcad-deployment-") or not root.name.startswith(
    "forcad-deployment."
):
    raise SystemExit("Expected an isolated deployment fixture")
compose = [
    "docker",
    "compose",
    "--env-file",
    str(root / "compose.env"),
    "--project-name",
    project,
    "--project-directory",
    str(root / "deployment/docker"),
    "-f",
    str(root / "deployment/docker/compose.yml"),
    "-f",
    str(Path(__file__).resolve().parents[3] / "deployment/docker/compose.e2e.yml"),
]
runtime_env_setting = os.environ.get("FORCAD_E2E_ADMIN_ENV_FILE")
runtime_env = Path(runtime_env_setting) if runtime_env_setting else root / "runtime.env"
if not runtime_env_setting and not runtime_env.exists():
    runtime_env = root / "config/runtime/admin.env"
require_private_file(runtime_env)
credentials = {
    key: value
    for line in runtime_env.read_text(encoding="utf-8").splitlines()
    if line and not line.startswith("#")
    for key, value in [line.split("=", 1)]
}
client = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
)
ACCEPTANCE_API_RPS = 25
ACCEPTANCE_API_INTERVAL = 1 / ACCEPTANCE_API_RPS
last_api_request_at = None
csrf_token = ""


def pace_api_request():
    global last_api_request_at
    now = time.monotonic()
    if last_api_request_at is not None:
        delay = ACCEPTANCE_API_INTERVAL - (now - last_api_request_at)
        if delay > 0:
            time.sleep(delay)
    last_api_request_at = time.monotonic()


def request(path, data=None, method="GET", token=None, extra_headers=None, csrf=True):
    headers = {"Content-Type": "application/json", "Origin": origin}
    if token:
        headers["X-Team-Token"] = token
    if (
        csrf
        and csrf_token
        and path.startswith("/api/v1/admin/")
        and path != "/api/v1/admin/login/"
        and method not in ("GET", "HEAD", "OPTIONS")
    ):
        headers["X-CSRF-Token"] = csrf_token
    headers.update(extra_headers or {})
    body = data
    if data is not None and not isinstance(data, bytes):
        body = json.dumps(data).encode()
    req = urllib.request.Request(
        origin + path,
        data=body,
        method=method,
        headers=headers,
    )
    if path.startswith("/api/"):
        pace_api_request()
    try:
        response = client.open(req, timeout=5)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        return response.status, response.read(1 << 20)


def query(sql):
    result = subprocess.run(
        compose
        + [
            "exec",
            "-T",
            "postgres",
            "sh",
            "-ec",
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
expect(
    request,
    "/api/v1/admin/login/",
    403,
    {"username": "invalid", "password": "invalid"},
    "POST",
)
expect(
    request,
    "/api/v1/admin/login/",
    415,
    {"username": "invalid", "password": "invalid"},
    "POST",
    extra_headers={"Content-Type": "text/plain"},
)
status, body = request(
    "/api/v1/admin/login/",
    {
        "username": credentials["ADMIN_USERNAME"],
        "password": credentials["ADMIN_PASSWORD"],
    },
    "POST",
)
assert status == 200, "admin login failed"
csrf_token = json.loads(body)["csrf_token"]
check_admin(request, csrf_token)

# The optional browser flow runs first and returns the game to waiting. Read the
# current generation from persisted state instead of assuming prior transitions.
status, _ = request(
    "/api/v1/admin/game/start_final/",
    {"confirm": True, "generation": current_generation()},
    "POST",
)
assert status == 200, "final did not start"
check_flag_input(request, credentials["FORCAD_E2E_TEAM_ONE_TOKEN"])
expect(request, "/api/v1/admin/game/pause/", data={}, method="POST")
expect(
    request, "/flags", 503, [], "PUT", token=credentials["FORCAD_E2E_TEAM_ONE_TOKEN"]
)
expect(request, "/api/v1/admin/game/resume/", data={}, method="POST")

flag = ""
round_time = int(query("SELECT round_time FROM GameConfig WHERE id=1;"))
checker_timeout = int(
    query("SELECT COALESCE(MAX(checker_timeout), 10) FROM Tasks WHERE active;")
)
flag_deadline = time.monotonic() + round_time + checker_timeout + 15
while time.monotonic() < flag_deadline:
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
    time.sleep(min(0.5, max(0, flag_deadline - time.monotonic())))
if len(flag) != 32:
    wait_seconds = round_time + checker_timeout + 15
    raise AssertionError(
        f"Python checker did not persist a flag within {wait_seconds} seconds"
    )
status, body = request(
    "/flags", [flag] * 100, "PUT", credentials["FORCAD_E2E_TEAM_ONE_TOKEN"]
)
assert (
    status == 200
    and body.count(b"Flag accepted!") == 1
    and body.count(b"Flag already stolen") == 99
), "batch verdicts incorrect"
assert query("SELECT count(*) FROM StolenFlags;") == "1", "duplicate scoring"
expect(
    request,
    "/flags/",
    data=[flag],
    method="PUT",
    token=credentials["FORCAD_E2E_TEAM_ONE_TOKEN"],
)
assert query("SELECT count(*) FROM StolenFlags;") == "1", "retry scored a flag twice"

address = urllib.parse.urlsplit(origin)
connection = http.client.HTTPConnection(address.hostname, address.port, timeout=5)
connection.request(
    "GET",
    "/api/v1/events/ws",
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
    "/api/v1/admin/game/finish/",
    {"confirm": True, "generation": current_generation()},
    "POST",
)
assert status == 200, "final did not finish"
assert (
    request("/flags", [flag], "PUT", credentials["FORCAD_E2E_TEAM_ONE_TOKEN"])[0] == 410
), "finished game accepts flags"

export_path = "/api/v1/admin/backup/export/?name=acceptance"
backup_deadline = time.monotonic() + checker_timeout + 15
while True:
    status, archive = request(export_path)
    if status != 409 or time.monotonic() >= backup_deadline:
        break
    time.sleep(0.25)
assert status == 200 and archive.startswith(b"\x1f\x8b"), f"event export failed: HTTP {status}"
backup_headers = {"Content-Type": "application/gzip"}
status, body = request(
    "/api/v1/admin/backup/inspect/", archive, "POST", extra_headers=backup_headers
)
assert status == 200 and json.loads(body)["name"] == "acceptance", "event preview failed"
status, _ = request(
    "/api/v1/admin/backup/restore/",
    archive,
    "POST",
    extra_headers={**backup_headers, "X-Forcad-Confirm": "restore"},
)
assert status == 409, "restore must refuse a played board"

# Restore onto a freshly seeded board inside this disposable Compose project.
# Leave the session cookie intact: importing an event must not import admin auth.
captured = query("SELECT flag_id::text || ':' || attacker_id::text FROM StolenFlags;")
query(
    """
BEGIN;
TRUNCATE GameRoundSnapshots, CheckerJobs, DispatchRuns, PublicAnnouncements,
    StolenFlags, Flags, TeamTasksLog, ScheduleHistory, CheckerActionLogs;
UPDATE GameConfig SET real_round = 0, start_time = '2050-01-01T00:00:00Z';
UPDATE GameSession SET final_start = NULL, paused_at = NULL, snapshot_round = NULL, snapshot = NULL;
UPDATE TeamTasks SET stolen = 0, lost = 0, checks = 0, checks_passed = 0;
COMMIT;
"""
)
status, _ = request(
    "/api/v1/admin/backup/restore/",
    archive,
    "POST",
    extra_headers={**backup_headers, "X-Forcad-Confirm": "restore"},
)
assert status == 200, f"event restore failed: HTTP {status}"
assert query("SELECT flag_id::text || ':' || attacker_id::text FROM StolenFlags;") == captured, (
    "restored capture does not match the exported game"
)
cli_archive = subprocess.run(
    [*compose, "exec", "-T", "api", "forcad", "backup", "operator"],
    capture_output=True,
    check=True,
    timeout=10,
).stdout
status, body = request(
    "/api/v1/admin/backup/inspect/", cli_archive, "POST", extra_headers=backup_headers
)
assert status == 200 and json.loads(body)["name"] == "operator", "CLI backup format differs"

expect(request, "/api/v1/admin/logout/", data={}, method="POST")
csrf_token = ""
expect(request, "/api/v1/admin/status/", 403)
expect(request, "/api/v1/admin/logout/", 403, {}, "POST")
assert request(export_path)[0] == 403, "unauthenticated backup export"
print(
    "Deployment checks passed: public reads, admin CRUD/auth/CSRF/validation, lifecycle, backup, batch/retry, Python checker, WebSocket, and private metrics."
)

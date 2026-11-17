"""Exercise public reads and administrator CRUD through the deployed proxy."""

import json
import re
import uuid


def expect(request, path, status=200, data=None, method="GET", **options):
    actual, body = request(path, data, method, **options)
    assert actual == status, f"{method} {path}: expected {status}, received {actual}"
    return json.loads(body) if body and status in (200, 201) else None


def check_public(request, team_id):
    paths = (
        "/api/v1/client/teams/",
        "/api/v1/client/tasks/",
        "/api/v1/client/config/",
        "/api/v1/client/attack_data/",
        "/api/v1/client/ctftime/",
        "/api/v1/client/status/",
        "/api/v1/client/scoreboard/",
        f"/api/v1/client/teams/{team_id}/",
    )
    for path in paths:
        payload = expect(request, path)
        serialized = json.dumps(payload)
        for private_field in (
            "token",
            "csrf_token",
            "private_message",
            "private_flag_data",
            "command",
            "checker",
        ):
            assert f'"{private_field}":' not in serialized, (
                f"{path} exposes {private_field}"
            )
    for path in (
        "/api/v1/client/health/",
        "/api/v1/admin/health/",
        "/api/v1/events/health/",
        "/flags/health/",
    ):
        assert expect(request, path) == {"status": "ok"}
    for path in (
        "/api/v1/client/metrics",
        "/api/v1/admin/metrics",
        "/api/v1/http-receiver/metrics",
    ):
        assert request(path)[0] == 404, f"{path} exposed publicly"
    expect(request, "/api/v1/client/teams/not-a-uuid/", 400)


def check_unauthorized(request, team_id):
    for path in ("/api/v1/admin/teams/", "/api/v1/admin/tasks/", "/api/v1/admin/status/"):
        expect(request, path, 403)
    for collection in ("teams", "tasks", "teamtasks"):
        expect(request, f"/api/v1/admin/{collection}/", 403, {}, "POST")
        for method in ("GET", "PUT", "DELETE"):
            expect(request, f"/api/v1/admin/{collection}/{team_id}/", 403, method=method)
    expect(request, "/api/v1/admin/game/start_practice/", 403, {}, "POST")
    expect(request, "/api/v1/admin/logout/", 403, {}, "POST")


def check_admin(request, csrf_token):
    session = expect(request, "/api/v1/admin/status/")
    assert re.fullmatch(r"[0-9a-f]{64}", csrf_token), "invalid session CSRF token"
    assert session["csrf_token"] == csrf_token, "session CSRF token changed"
    wrong_csrf = ("0" if csrf_token[0] != "0" else "1") + csrf_token[1:]
    # Invalid input keeps this rejection check harmless even if the guard regresses.
    for path in ("/api/v1/admin/teams/", "/api/v1/admin/logout/"):
        expect(request, path, 403, {}, "POST", csrf=False)
        expect(
            request,
            path,
            403,
            {},
            "POST",
            extra_headers={"X-CSRF-Token": wrong_csrf},
        )
    assert expect(request, "/api/v1/admin/status/")["csrf_token"] == csrf_token, (
        "rejected mutation changed the session"
    )
    teams = expect(request, "/api/v1/admin/teams/")
    tasks = expect(request, "/api/v1/admin/tasks/")
    assert teams and tasks, "deployment fixture has no team or task"
    check_team(request, teams[0])
    check_task(request, tasks[0])
    path = f"/api/v1/admin/teamtasks/?team_id={teams[0]['id']}&task_id={tasks[0]['id']}"
    assert isinstance(expect(request, path), list)
    expect(request, "/api/v1/admin/teamtasks/", 400)
    expect(request, "/api/v1/admin/teamtasks/", 404, method="POST")
    for method in ("GET", "PUT", "DELETE"):
        expect(request, f"/api/v1/admin/teamtasks/{uuid.uuid4()}/", 404, method=method)
    expect(request, "/api/v1/admin/game/start_final/", 400, {}, "POST")
    expect(request, "/api/v1/admin/game/finish/", 400, {}, "POST")
    expect(
        request,
        "/api/v1/admin/game/start_final/",
        409,
        {"confirm": True, "generation": -1},
        "POST",
    )
    expect(
        request,
        "/api/v1/admin/game/start_practice/",
        403,
        {},
        "POST",
        extra_headers={"Origin": "https://untrusted.example"},
    )


def check_team(request, existing):
    collection = "/api/v1/admin/teams/"
    expect(request, collection, 400, {"name": "invalid", "ip": "not-an-ip"}, "POST")
    expect(
        request,
        collection,
        400,
        {"name": "invalid", "ip": "192.0.2.3", "logo_path": "../private"},
        "POST",
    )
    created = expect(
        request,
        collection,
        201,
        {"name": "acceptance", "ip": "192.0.2.3", "token": "ignored"},
        "POST",
    )
    team_id = str(uuid.UUID(created["id"]))
    assert re.fullmatch(r"[0-9a-f]{16}", created["token"]), (
        "server did not generate a team token"
    )
    path = f"{collection}{team_id}/"
    assert expect(request, path) == created
    updated = {**created, "name": "acceptance updated", "highlighted": True}
    assert expect(request, path, data=updated, method="PUT")["name"] == updated["name"]
    expect(request, path, 409, {**updated, "token": existing["token"]}, "PUT")
    assert expect(request, path)["token"] == created["token"], (
        "failed update changed token"
    )
    expect(request, path, method="DELETE")
    assert not expect(request, path)["active"], "delete did not deactivate the team"
    assert all(team["id"] != team_id for team in expect(request, "/api/v1/client/teams/"))
    missing = f"{collection}{uuid.uuid4()}/"
    expect(request, missing, 404)
    expect(request, missing, 404, updated, "PUT")
    expect(request, missing, 404, method="DELETE")
    expect(request, f"{collection}not-a-uuid/", 400)


def check_task(request, existing):
    collection = "/api/v1/admin/tasks/"
    data = {
        **existing,
        "name": "acceptance",
        "default_score": int(existing["default_score"]),
    }
    data.pop("id")
    for invalid in (
        {"places": 0},
        {"puts": -1},
        {"checker_timeout": 0},
        {"default_score": 1.5},
    ):
        expect(request, collection, 400, {**data, **invalid}, "POST")
    created = expect(request, collection, 201, data, "POST")
    task_id = str(uuid.UUID(created["id"]))
    path = f"{collection}{task_id}/"
    assert expect(request, path) == created
    updated = {**data, "name": "acceptance updated", "places": 2}
    assert expect(request, path, data=updated, method="PUT")["places"] == 2
    expect(request, path, method="DELETE")
    assert not expect(request, path)["active"], "delete did not deactivate the task"
    assert all(task["id"] != task_id for task in expect(request, "/api/v1/client/tasks/"))
    missing = f"{collection}{uuid.uuid4()}/"
    expect(request, missing, 404)
    expect(request, missing, 404, updated, "PUT")
    expect(request, missing, 404, method="DELETE")
    expect(request, f"{collection}not-a-uuid/", 400)


def check_flag_input(request, token):
    for data in (["invalid"], ["A" * 32]):
        expect(request, "/flags", 400, data, "PUT", token="invalid")
    for data in ({"flag": "invalid"}, ["A" * 33], ["A" * 32] * 101):
        expect(request, "/flags", 400, data, "PUT", token=token)
    expect(request, "/flags", 413, ["A" * 40000], "PUT", token=token)
    expect(
        request,
        "/flags",
        415,
        [],
        "PUT",
        token=token,
        extra_headers={"Content-Type": "text/plain"},
    )

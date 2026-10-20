"""Synchronize demo teams without resetting scores or existing tokens."""

import http.cookiejar
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

import yaml


def sync_teams(request, configured, output):
    existing = request("/api/admin/teams/")
    pending_path = output / "teams-pending.json"
    pending = json.loads(pending_path.read_text()) if pending_path.exists() else {}
    by_token = {team["token"]: team for team in existing}
    desired_tokens = {team["token"] for team in configured}
    desired_by_ip = {team["ip"]: team for team in configured}
    # A journal entry permits recovery after POST succeeded but PUT was interrupted.
    for current in existing:
        if current["token"] in desired_tokens:
            continue
        planned = pending.get(current["ip"])
        desired = desired_by_ip.get(current["ip"])
        if (
            planned is None
            or desired is None
            or planned != {"name": desired["name"], "token": desired["token"]}
            or current["name"] != planned["name"]
        ):
            raise SystemExit(
                "Demo database contains an unrecognized team; no teams changed"
            )
    updates, additions = [], []
    for index, team in enumerate(configured):
        current = by_token.get(team["token"])
        if current is None:
            current = next(
                (item for item in existing if item["ip"] == team["ip"]), None
            )
            if current is None:
                if any(item["name"] == team["name"] for item in existing):
                    raise SystemExit("Demo team name collision; no teams changed")
                additions.append(team)
            else:
                updates.append({**current, "token": team["token"]})
            continue
        allowed = {team["ip"]}
        if index < 2:
            allowed.update((f"172.30.240.{11 + index}", f"10.{101 + index}.0.2"))
        if current["ip"] not in allowed:
            raise SystemExit("Existing team address is not part of the demo")
        if current["ip"] != team["ip"]:
            updates.append({**current, "ip": team["ip"]})
    if additions or updates:
        backup = output / "teams-before.json"
        if not backup.exists():
            backup.write_text(json.dumps(existing, indent=2))
            backup.chmod(0o600)
    for team in additions:
        pending[team["ip"]] = {"name": team["name"], "token": team["token"]}
    if pending:
        pending_path.write_text(json.dumps(pending, indent=2))
        pending_path.chmod(0o600)
    for team in additions:
        current = request(
            "/api/admin/teams/",
            {
                "name": team["name"],
                "ip": team["ip"],
                "active": True,
                "highlighted": team.get("highlighted", False),
                "logo_path": team.get("logo", ""),
            },
            "POST",
        )
        updates.append({**current, "token": team["token"]})
    for team in updates:
        request(f"/api/admin/teams/{team['id']}/", team, "PUT")
    pending_path.unlink(missing_ok=True)
    print(
        f"Demo teams synchronized ({len(additions)} added, {len(updates)} updated); existing scores and tokens retained."
    )


def main():
    root = Path(sys.argv[1])
    configured = yaml.safe_load((root / "config/config.yml").read_text())["teams"]
    credentials = dict(
        line.split("=", 1)
        for line in (root / "config/runtime/admin.env").read_text().splitlines()
        if line and not line.startswith("#")
    )
    client = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
    )

    def request(path, data=None, method="GET"):
        req = urllib.request.Request(
            "http://localhost:8080" + path,
            data=json.dumps(data).encode() if data is not None else None,
            method=method,
            headers={
                "Content-Type": "application/json",
                "Origin": "http://localhost:8080",
            },
        )
        try:
            with client.open(req, timeout=5) as response:
                payload = response.read()
                return json.loads(payload) if payload else None
        except urllib.error.HTTPError as exc:
            raise SystemExit(
                f"Demo API {method} {path} returned {exc.code}; no credentials were printed"
            ) from None

    request(
        "/api/admin/login/",
        {
            "username": credentials["ADMIN_USERNAME"],
            "password": credentials["ADMIN_PASSWORD"],
        },
        "POST",
    )
    sync_teams(request, configured, root / "config/runtime/demo")


if __name__ == "__main__":
    os.umask(0o077)
    main()

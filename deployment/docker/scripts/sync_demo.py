"""Move the two existing demo teams to their routed addresses without resetting a game."""

import http.cookiejar
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

import yaml


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
    existing = request("/api/admin/teams/")
    if len(existing) != 2 or len(configured) != 2:
        raise SystemExit(
            "Refusing to modify a database that does not contain exactly two demo teams"
        )
    by_token = {team["token"]: team for team in existing}
    if {team["token"] for team in configured} != set(by_token):
        raise SystemExit(
            "Demo team tokens do not match the database; existing data left untouched"
        )
    updates = []
    for team in configured:
        current = by_token[team["token"]]
        if current["ip"] not in {
            "172.30.240.11",
            "172.30.240.12",
            "10.101.0.2",
            "10.102.0.2",
        }:
            raise SystemExit("Existing team address is not part of the demo")
        if current["ip"] != team["ip"]:
            updates.append({**current, "ip": team["ip"]})
    if updates:
        backup = root / "config/runtime/demo/teams-before.json"
        if not backup.exists():
            backup.write_text(json.dumps(existing, indent=2))
        for team in updates:
            request(f"/api/admin/teams/{team['id']}/", team, "PUT")
    print(
        f"Demo team addresses synchronized ({len(updates)} updated); scores, tokens and session retained."
    )


if __name__ == "__main__":
    os.umask(0o077)
    main()

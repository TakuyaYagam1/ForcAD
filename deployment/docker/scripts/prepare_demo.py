"""Prepare the private demo using the infrastructure renderer."""

import json
import os
import posixpath
import re
import secrets
import shutil
import sys
from pathlib import Path

import tomllib
import yaml

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "infrastracture"))
import render
from demo_compose import compose_teams
from demo_keys import prepare_keys

OLD_IPS = (["172.30.240.11", "172.30.240.12"], ["10.101.0.2", "10.102.0.2"])


def validate_settings(output, settings_path):
    if not output.exists():
        return
    marker = output / "settings.toml"
    if not marker.is_file():
        raise SystemExit("Existing demo bundle has no settings.toml")
    old = tomllib.loads(marker.read_text())
    new = tomllib.loads(settings_path.read_text())
    previous = old["teams"].pop("entries")
    current = new["teams"].pop("entries")
    if old != new or current[: len(previous)] != previous:
        raise SystemExit(
            "Existing demo settings may only append teams; topology edits require a separate demo"
        )


def game_matches(document, generated):
    if not isinstance(document, dict):
        return False
    game = document.get("game")
    tasks = document.get("tasks")
    if not isinstance(game, dict) or not isinstance(tasks, list):
        return False
    if len(tasks) != len(generated["tasks"]) or any(
        not isinstance(task, dict) for task in tasks
    ):
        return False
    for key, value in generated["game"].items():
        # An explicit period on every task makes the game default unused.
        if (
            key == "get_period"
            and game.get(key) is None
            and all(task.get("get_period") is not None for task in tasks)
        ):
            continue
        if game.get(key, 0 if key == "extra_checks" else None) != value:
            return False
    for actual, expected in zip(tasks, generated["tasks"]):
        for key, value in expected.items():
            if key == "checker":
                value = posixpath.join(generated["game"]["checkers_path"], value)
            current = actual.get(key)
            if (
                key == "checker"
                and isinstance(current, str)
                and not current.startswith("/")
            ):
                current = posixpath.join(game["checkers_path"], current)
            if current != value:
                return False
    return True


def migrate_wireguard(output):
    """Renumber existing demo peers without rotating keys."""
    wireguard = output / "wireguard"
    updates = {}
    for path in [*wireguard.rglob("*.conf"), wireguard / "assignments.csv"]:
        original = path.read_text()
        content = re.sub(r"\b10\.(200|201)\.0\.", r"10.183.\1.", original)
        content = content.replace(
            "AllowedIPs = 10.0.0.0/8", "AllowedIPs = 10.183.0.0/16"
        )
        if content != original:
            updates[path] = content
    if updates:
        backup = output / "wireguard-before-network"
        if not backup.exists():
            shutil.copytree(wireguard, backup)
        for path, content in updates.items():
            path.write_text(content)


def main():
    root = Path(sys.argv[1]).resolve()
    config = root / "config/config.yml"
    settings_path = root / "deployment/demo/settings.toml"
    network = render.load_config(settings_path)
    generated = yaml.safe_load(render.forcad_config(network))
    existing = config.read_bytes() if config.exists() else None
    document = yaml.safe_load(existing) if existing is not None else generated
    database_exists = (root / "docker_volumes/postgres/data").exists()
    if (
        existing is not None
        and database_exists
        and not game_matches(document, generated)
    ):
        raise SystemExit(
            "Existing demo game config differs from settings.toml; keep the database and config together"
        )
    teams = sorted(
        document["teams"], key=lambda team: tuple(map(int, team["ip"].split(".")))
    )
    inventory = render.teams(network)
    new_ips = [team["vulnbox"] for team in inventory]
    if not 2 <= len(inventory) <= 32:
        raise SystemExit("Demo supports 2..32 teams")
    if len(teams) > len(inventory) or [team["ip"] for team in teams] not in (
        *OLD_IPS,
        new_ips[: len(teams)],
    ):
        raise SystemExit("Existing teams are not a prefix of the demo inventory")
    runtime = root / "config/runtime"
    output = runtime / "demo"
    validate_settings(output, settings_path)
    changed = existing is None
    previous_count = len(teams)
    for team in generated["teams"][previous_count:]:
        teams.append(team)
        changed = True
    document["teams"] = teams
    if not database_exists:
        for section in ("game", "tasks"):
            if document.get(section) != generated[section]:
                document[section] = generated[section]
                changed = True
    for index, team in enumerate(teams):
        if not database_exists:
            for key, value in generated["teams"][index].items():
                if (
                    key not in {"ip", "traffic_cidrs", "token"}
                    and team.get(key) != value
                ):
                    team[key] = value
                    changed = True
        if team.get("token") is None:
            if database_exists and index < previous_count:
                raise SystemExit(
                    "Existing database: copy its team tokens into config.yml before using the demo"
                )
            team["token"] = secrets.token_hex(8)
            changed = True
        if not re.fullmatch(r"[0-9a-f]{16}", team["token"]):
            raise SystemExit("Expected a 16-character hexadecimal team token")
        cidrs = [new_ips[index] + "/32", inventory[index]["vpn_cidr"]]
        if team["ip"] != new_ips[index] or team.get("traffic_cidrs") != cidrs:
            team.update(ip=new_ips[index], traffic_cidrs=cidrs)
            changed = True
    runtime.mkdir(mode=0o700, exist_ok=True)
    files = render.render(network)
    files["compose.yml"] = compose_teams(root, inventory)
    files["network.json"] = (
        json.dumps(
            {
                "teams": [
                    {"team_ip": team["ip"], "networks": team["traffic_cidrs"]}
                    for team in teams
                ]
            },
            indent=2,
        )
        + "\n"
    )
    prometheus = yaml.safe_load(
        (root / "deployment/monitoring/prometheus.yml").read_text()
    )
    for job in prometheus["scrape_configs"]:
        if job["job_name"] == "forcad_worker":
            job.pop("dns_sd_configs")
            job["static_configs"] = [{"targets": ["localhost:9091"]}]
    files["prometheus.yml"] = yaml.safe_dump(prometheus, sort_keys=False)
    if not output.exists():
        # Demo starts in game mode so the traffic graphs are populated.
        render.write_bundle(output, {**files, "phase": "game\n"})
    else:
        for relative, content in files.items():
            path = output / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            if isinstance(content, bytes):
                path.write_bytes(content)
            else:
                path.write_text(content)
    if (output / "wireguard").exists():
        migrate_wireguard(output)
    prepare_keys(network, output)
    for number in range(1, len(teams) + 1):
        (output / "seeds" / f"team{number}").mkdir(
            parents=True, exist_ok=True, mode=0o700
        )
    # Bind only these public files into the non-root collectors.
    for relative in (
        "prometheus.yml",
        "forcad/routers.yml",
        "forcad/game-sensors.yml",
        "router/game-metrics.zeek",
        "router/game-inventory.zeek",
    ):
        (output / relative).chmod(0o644)
    if changed:
        if existing is not None:
            backup = root / "config/config_backup_demo_network.yml"
            if not backup.exists():
                backup.write_bytes(existing)
        config.write_text(yaml.safe_dump(document, sort_keys=False, allow_unicode=True))
    grafana = runtime / "demo.env"
    if not grafana.exists():
        grafana.write_text("GRAFANA_ADMIN_PASSWORD=" + secrets.token_hex(24) + "\n")
    for index, team in enumerate(teams):
        (runtime / f"demo-team{index + 1}.env").write_text(
            f"DEMO_TOKEN={team['token']}\nDEMO_PEER={teams[(index + 1) % len(teams)]['ip']}\n"
        )
    (output / "settings.toml").write_text(settings_path.read_text())
    print(
        f"Prepared router, VPN, {len(teams)} teams and players, admin VPN and game sensor. Existing keys and game data retained."
    )


if __name__ == "__main__":
    os.umask(0o077)
    main()

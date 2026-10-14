"""Prepare the private two-team demo using the infrastructure renderer."""

import json
import os
import re
import secrets
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "infrastracture"))
import render
from vpn.generate import generate

OLD_IPS = ["172.30.240.11", "172.30.240.12"]
NEW_IPS = ["10.101.0.2", "10.102.0.2"]


def main():
    root = Path(sys.argv[1]).resolve()
    config = root / "config/config.yml"
    document = yaml.safe_load(config.read_text())
    teams = sorted(document["teams"], key=lambda team: team["ip"])
    if [team["ip"] for team in teams] not in (OLD_IPS, NEW_IPS):
        raise SystemExit(
            "Demo requires exactly two demo teams. See deployment/demo/README.md"
        )
    network_path = root / "deployment/demo/network.toml"
    network = render.load_config(network_path)
    inventory = render.teams(network)
    changed = False
    for index, team in enumerate(teams):
        if team.get("token") is None:
            if (root / "docker_volumes/postgres/data").exists():
                raise SystemExit(
                    "Existing database: copy its team tokens into config.yml before using the demo"
                )
            team["token"] = secrets.token_hex(8)
            changed = True
        if not re.fullmatch(r"[0-9a-f]{16}", team["token"]):
            raise SystemExit("Expected a 16-character hexadecimal team token")
        cidrs = [NEW_IPS[index] + "/32", inventory[index]["vpn_cidr"]]
        if team["ip"] != NEW_IPS[index] or team.get("traffic_cidrs") != cidrs:
            team.update(ip=NEW_IPS[index], traffic_cidrs=cidrs)
            changed = True
    runtime = root / "config/runtime"
    runtime.mkdir(mode=0o700, exist_ok=True)
    output = runtime / "demo"
    if output.exists() and (
        not (output / "network.toml").is_file()
        or (output / "network.toml").read_text() != network_path.read_text()
    ):
        raise SystemExit(
            "Existing demo bundle differs from network.toml; keep it for rollback and choose a fresh output before changing topology"
        )
    files = render.render(network)
    files["network.toml"] = network_path.read_text()
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
            path.write_text(content)
    if not (output / "wireguard").exists():
        generate(network, output / "wireguard")
    for number in (1, 2):
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
        backup = root / "config/config_backup_demo_network.yml"
        if not backup.exists():
            backup.write_bytes(config.read_bytes())
        config.write_text(yaml.safe_dump(document, sort_keys=False, allow_unicode=True))
    grafana = runtime / "demo.env"
    if not grafana.exists():
        grafana.write_text("GRAFANA_ADMIN_PASSWORD=" + secrets.token_hex(24) + "\n")
    for index, team in enumerate(teams):
        (runtime / f"demo-team{index + 1}.env").write_text(
            f"DEMO_TOKEN={team['token']}\nDEMO_PEER={teams[1 - index]['ip']}\n"
        )
    print(
        "Prepared router, VPN, two players, admin VPN and game sensor. Existing keys and game data retained."
    )


if __name__ == "__main__":
    os.umask(0o077)
    main()

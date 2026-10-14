"""Prepare private credentials for the two-team local demo."""
import json
import os
from pathlib import Path
import re
import secrets
import sys

import yaml


def main():
    root = Path(sys.argv[1])
    config = root / "config/config.yml"
    document = yaml.safe_load(config.read_text())
    teams = sorted(document["teams"], key=lambda team: team["ip"])
    if [team["ip"] for team in teams] != ["172.30.240.11", "172.30.240.12"]:
        raise SystemExit("Demo requires exactly two teams: 172.30.240.11 and 172.30.240.12. See deployment/demo/README.md")
    changed = False
    for team in teams:
        if team.get("token") is None:
            if (root / "docker_volumes/postgres/data").exists():
                raise SystemExit("Existing database: copy its team tokens into config.yml before using the demo")
            team["token"] = secrets.token_hex(8)
            changed = True
        if not re.fullmatch(r"[0-9a-f]{16}", team["token"]):
            raise SystemExit("Expected a 16-character hexadecimal team token")
    if changed:
        backup = root / "config/config_backup_demo.yml"
        if not backup.exists():
            backup.write_bytes(config.read_bytes())
        config.write_text(yaml.safe_dump(document, sort_keys=False, allow_unicode=True))
    runtime = root / "config/runtime"
    runtime.mkdir(mode=0o700, exist_ok=True)
    grafana = runtime / "demo.env"
    if not grafana.exists():
        grafana.write_text("GRAFANA_ADMIN_PASSWORD=" + secrets.token_hex(24) + "\n")
    for index, team in enumerate(teams):
        (runtime / f"demo-team{index + 1}.env").write_text(
            f"DEMO_TOKEN={team['token']}\nDEMO_PEER={teams[1 - index]['ip']}\n"
        )
    inventory = {"teams": [{"team_ip": team["ip"], "networks": team.get("traffic_cidrs") or [team["ip"] + "/32"]} for team in teams]}
    network = runtime / "demo-network.json"
    network.write_text(json.dumps(inventory))
    network.chmod(0o644)  # Public inventory, read by the router without DAC capabilities.


if __name__ == "__main__":
    os.umask(0o077)
    main()

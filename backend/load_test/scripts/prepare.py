#!/usr/bin/env python3
"""Write a synthetic game configuration into the isolated deployment."""

import os
import sys
from pathlib import Path


def document():
    team_count = int(os.environ.get("LOAD_TEAM_COUNT", "20"))
    if not 2 <= team_count <= 28:
        raise ValueError("LOAD_TEAM_COUNT must be between 2 and 28")
    round_time = int(os.environ.get("LOAD_ROUND_TIME", "10"))
    checker_timeout = int(os.environ.get("LOAD_CHECKER_TIMEOUT", "5"))
    if not 10 <= round_time <= 120 or not 5 <= checker_timeout < round_time:
        raise ValueError("LOAD_ROUND_TIME and LOAD_CHECKER_TIMEOUT are invalid")
    puts = [int(value) for value in os.environ.get("LOAD_PUTS", "1,1,1").split(",")]
    if len(puts) != 3 or any(not 1 <= value <= 1023 for value in puts):
        raise ValueError("LOAD_PUTS must contain three integers between 1 and 1023")
    content = f"""game:
  mode: classic
  start_time: "2050-01-01 00:00:00"
  timezone: Europe/Moscow
  round_time: {round_time}
  rounds: null
  flag_lifetime: 5
tasks:
"""
    for name, put_count in zip(("notes", "vault", "queue"), puts, strict=True):
        content += f"  - name: {name}\n    checker: {name}.py\n    checker_timeout: {checker_timeout}\n    puts: {put_count}\n    places: {put_count}\n    gets: 1\n"
    content += "teams:\n"
    for team in range(1, team_count + 1):
        content += f'  - name: team-{team:02}\n    ip: 192.0.2.{team}\n    token: "{team:016x}"\n'
    return content


if __name__ == "__main__":
    Path(sys.argv[1]).write_text(document(), encoding="utf-8")

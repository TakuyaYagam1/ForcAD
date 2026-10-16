#!/usr/bin/env python3
"""Write a synthetic game configuration into the isolated deployment."""

import sys
from pathlib import Path


def document():
    content = """game:
  mode: classic
  start_time: "2050-01-01 00:00:00"
  timezone: Europe/Moscow
  round_time: 10
  rounds: null
  flag_lifetime: 5
tasks:
"""
    for name in ("notes", "vault", "queue"):
        content += f"  - name: {name}\n    checker: {name}.py\n    checker_timeout: 5\n    puts: 1\n    gets: 1\n"
    content += "teams:\n"
    for team in range(1, 21):
        content += f'  - name: team-{team:02}\n    ip: 192.0.2.{team}\n    token: "{team:016x}"\n'
    return content


if __name__ == "__main__":
    Path(sys.argv[1]).write_text(document(), encoding="utf-8")

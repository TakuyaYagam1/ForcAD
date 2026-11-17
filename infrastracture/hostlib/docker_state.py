"""Capture and undo firewall changes made by a newly installed Docker daemon."""

import argparse
import collections
import json
import shlex
import shutil
import subprocess

from .common import regular

DOCKER_CHAINS = {
    "DOCKER",
    "DOCKER-BRIDGE",
    "DOCKER-CT",
    "DOCKER-FORWARD",
    "DOCKER-INGRESS",
    "DOCKER-INTERNAL",
    "DOCKER-ISOLATION-STAGE-1",
    "DOCKER-ISOLATION-STAGE-2",
    "DOCKER-USER",
}


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout


def capture():
    tables = {}
    for tool in ("iptables", "ip6tables"):
        for table in ("filter", "nat"):
            key = f"{tool}:{table}"
            tables[key] = (
                [
                    shlex.split(line)
                    for line in run(tool, "-w", "-t", table, "-S").splitlines()
                ]
                if shutil.which(tool)
                else []
            )
    links = json.loads(run("ip", "-j", "-d", "link", "show"))
    bridge = next((link for link in links if link["ifname"] == "docker0"), None)
    return {"tables": tables, "bridge": bridge}


def restore(before, applied, current):
    """Plan all changes first; keep unrelated rules and reject reused chains."""
    commands = []
    for tool in ("iptables", "ip6tables"):
        for table in ("filter", "nat"):
            key = f"{tool}:{table}"
            original = [tuple(row) for row in before["tables"][key]]
            installed = [tuple(row) for row in applied["tables"][key]]
            live = [tuple(row) for row in current["tables"][key]]
            added = collections.Counter(installed) - collections.Counter(original)
            owned = {
                row[1] for row in added if row[0] == "-N" and row[1] in DOCKER_CHAINS
            }
            extra = collections.Counter(live) - collections.Counter(installed)
            for row in extra:
                if row[0] == "-A" and (
                    row[1] in owned
                    or any(
                        row[i + 1] in owned
                        for i, arg in enumerate(row[:-1])
                        if arg in {"-j", "-g"}
                    )
                ):
                    raise ValueError(f"Additional rules use Docker chains in {key}")
            prefix = [tool, "-w", "-t", table]
            remaining = collections.Counter(live)
            for row, count in added.items():
                if row[0] != "-A":
                    continue
                references = {
                    row[i + 1] for i, arg in enumerate(row[:-1]) if arg in {"-j", "-g"}
                }
                interfaces = {
                    row[i + 1] for i, arg in enumerate(row[:-1]) if arg in {"-i", "-o"}
                }
                docker_bridge = (
                    before["bridge"] is None
                    and applied["bridge"] is not None
                    and "docker0" in interfaces
                )
                if (
                    row[1] not in owned
                    and not (references & owned)
                    and not docker_bridge
                ):
                    continue
                for _ in range(min(count, remaining[row])):
                    commands.append([*prefix, "-D", *row[1:]])
                    remaining[row] -= 1
            live_chains = {row[1] for row in live if row[0] == "-N"}
            for chain in sorted(owned & live_chains):
                commands.append([*prefix, "-X", chain])
            for row in installed:
                if row[:2] != ("-P", "FORWARD"):
                    continue
                previous = next(
                    (old for old in original if old[:2] == row[:2]),
                    ("-P", row[1], "ACCEPT"),
                )
                if previous != row and row in live:
                    commands.append([*prefix, *previous])
    old_bridge, bridge = before["bridge"], applied["bridge"]
    live_bridge = current["bridge"]
    if old_bridge is None and bridge is not None and live_bridge is not None:
        if (
            any(
                bridge.get(key) != live_bridge.get(key)
                for key in ("ifindex", "address")
            )
            or live_bridge.get("linkinfo", {}).get("info_kind") != "bridge"
        ):
            raise ValueError("docker0 changed after installation")
        links = json.loads(run("ip", "-j", "link", "show"))
        if any(
            link.get("master") in {"docker0", live_bridge["ifindex"]} for link in links
        ):
            raise ValueError("Other interfaces still use docker0")
        commands.append(["ip", "link", "delete", "docker0"])
    for command in commands:
        run(*command)


def main():
    from pathlib import Path

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("capture", "restore"))
    parser.add_argument("before", nargs="?", type=Path)
    parser.add_argument("applied", nargs="?", type=Path)
    args = parser.parse_args()
    if args.action == "capture":
        print(json.dumps(capture()))
    else:
        if args.before is None or args.applied is None:
            parser.error("restore requires the before and applied snapshots")
        restore(
            json.loads(regular(args.before).read_text()),
            json.loads(regular(args.applied).read_text()),
            capture(),
        )


if __name__ == "__main__":
    main()

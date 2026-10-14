"""Configure only this demo container's network namespace, then drop privileges."""

import os
import shutil
import subprocess
import sys
from pathlib import Path


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def wireguard(name, path):
    # Feed keys through stdin, never process arguments or console output.
    lines = Path(path).read_text().splitlines()
    address = next(
        line.split("=", 1)[1].strip() for line in lines if line.startswith("Address")
    )
    configuration = (
        "\n".join(line for line in lines if not line.startswith("Address")) + "\n"
    )
    run("ip", "link", "add", name, "type", "wireguard")
    run(
        "wg",
        "setconf",
        name,
        "/dev/stdin",
        input=configuration,
        text=True,
        capture_output=True,
    )
    run("ip", "address", "add", address, "dev", name)
    run("ip", "link", "set", name, "mtu", "1380", "up")


def unprivileged(*command):
    os.execvp(
        "setpriv",
        [
            "setpriv",
            "--reuid=10001",
            "--regid=10001",
            "--clear-groups",
            "--bounding-set=-all",
            "--inh-caps=-all",
            "--ambient-caps=-all",
            *command,
        ],
    )


def main():
    role = sys.argv[1]
    if role == "router":
        for subnet in ("10.200.0.0/24", "10.201.0.0/24"):
            run("ip", "route", "replace", subnet, "via", "10.0.1.2", "dev", "eth1")
        phase = Path("/etc/forcad/phase").read_text().strip()
        if phase not in {"game", "pre-game"}:
            raise ValueError("Unknown demo network phase")
        run("nft", "-f", f"/etc/forcad/router/{phase}.nft")
        rules = run(
            "forcad",
            "network-rules",
            "--config",
            "/etc/forcad/network.json",
            capture_output=True,
        ).stdout
        run("nft", "-f", "-", input=rules)
        # The exporter needs NET_ADMIN to read nftables counters in this namespace.
        os.execvp(
            "forcad",
            [
                "forcad",
                "network-metrics",
                "--config",
                "/etc/forcad/network.json",
                "--listen",
                "10.0.2.1:9109",
            ],
        )
    elif role == "vpn":
        run("nft", "-f", "/etc/forcad/vpn/firewall.nft")
        run("ip", "route", "replace", "default", "via", "10.0.1.1")
        wireguard("wg-attack", "/app/keys/wg-attack.conf")
        wireguard("wg-admin", "/app/keys/wg-admin.conf")
        unprivileged("sleep", "infinity")
    elif role == "player":
        wireguard("wg0", "/app/keys/wg0.conf")
        run("ip", "route", "replace", "10.0.0.0/8", "dev", "wg0")
        unprivileged("sleep", "infinity")
    elif role == "team":
        run("ip", "route", "replace", "default", "via", os.environ["DEMO_GATEWAY"])
        os.chown("/data", 10001, 10001)
        if not Path("/data/note.db").exists() and Path("/app/seed/note.db").is_file():
            shutil.copyfile("/app/seed/note.db", "/data/note.db")
            os.chown("/data/note.db", 10001, 10001)
        unprivileged("python", "service.py")
    elif role == "forcad":
        run("ip", "route", "replace", "10.0.0.0/8", "via", "10.0.2.1", "dev", "eth1")
        # Nginx listens unprivileged on 8080; participants use board TCP/80.
        run(
            "nft",
            "-f",
            "-",
            input=(
                "table ip demo_board {\n chain input {\n type nat hook prerouting priority dstnat;\n"
                'iifname "eth1" ip daddr 10.0.2.10 tcp dport 80 redirect to :8080\n }\n}\n'
            ),
            text=True,
        )
        unprivileged("sleep", "infinity")
    else:
        raise ValueError("Unknown demo network role")


if __name__ == "__main__":
    main()

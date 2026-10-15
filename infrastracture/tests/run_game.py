#!/usr/bin/env python3
"""Check real Zeek TCP reassembly and HTTP/WS metrics in an isolated container."""

import json
import os
import shlex
import subprocess
import sys
import tempfile
from pathlib import Path

from game_trace import fixture

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
import render

IMAGE = "zeek/zeek@sha256:70733f4e540ba1608e37e00c6a93d009f79734262c9ec2ba09d90aa9abc16de5"  # 9.0.0


def container(command, check=True, **kwargs):
    with tempfile.TemporaryDirectory(prefix="forcad-container-") as directory:
        cid = Path(directory) / "container.cid"
        try:
            return subprocess.run(
                [*command[:2], "--cidfile", str(cid), *command[2:]],
                check=check,
                **kwargs,
            )
        finally:
            if cid.exists():
                subprocess.run(
                    ["docker", "rm", "-f", cid.read_text().strip()],
                    check=False,
                    capture_output=True,
                    timeout=20,
                )


def main():
    # Pull separately so Docker progress does not look like a Zeek diagnostic.
    image = subprocess.run(
        ["docker", "image", "inspect", IMAGE],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        timeout=30,
    )
    if image.returncode:
        subprocess.run(["docker", "pull", IMAGE], check=True, timeout=180)
    config = render.load_config(ROOT / "network.example.toml")
    config.update(teams=2, members_per_team=2, game_udp_ports=[31337])
    with tempfile.TemporaryDirectory(prefix="forcad-game-") as directory:
        work = Path(directory)
        for name, content in render.game_metrics(config).items():
            if name.endswith(".zeek"):
                # Offline tests do not bind to a host-specific management address.
                content = content.replace('"10.183.2.1"', '"127.0.0.1"')
                (work / Path(name).name).write_text(content)
        fixture().write(work / "game.pcap")
        (work / "check.zeek").write_text("""event net_done(t: time) {
    print "SNAPSHOT " + to_json(Telemetry::collect_metrics("forcad_game"));
}
event zeek_done() {
    print "FINAL " + to_json(Telemetry::collect_metrics("forcad_game"));
}
""")
        command = [
            "docker",
            "run",
            "--rm",
            "--network",
            "none",
            "--read-only",
            "--cap-drop",
            "ALL",
            "--security-opt",
            "no-new-privileges",
            "--workdir",
            "/work",
            "--mount",
            f"type=bind,src={work},dst=/work,readonly",
            "--entrypoint",
            "/bin/sh",
            IMAGE,
            "-c",
            "pwd -P && exec zeek -b -r game.pcap game-metrics.zeek game-inventory.zeek check.zeek",
        ]
        result = container(
            command, check=True, capture_output=True, text=True, timeout=60
        )
        snapshots = {}
        for line in result.stdout.splitlines():
            if line.startswith(("SNAPSHOT ", "FINAL ")):
                phase, document = line.split(" ", 1)
                snapshots[phase] = json.loads(document)
        assert set(snapshots) == {"SNAPSHOT", "FINAL"}, result.stdout + result.stderr

        def metric(phase, name, source="10.183.101.2", target="10.183.102.2"):
            for item in snapshots[phase]:
                labels = dict(
                    zip(item["label_names"], item["label_values"], strict=True)
                )
                if (
                    item["opts"]["name"] == "forcad_game_" + name
                    and labels.get("source_team_ip") == source
                    and labels.get("team_ip") == target
                ):
                    return item["value"]
            raise AssertionError(f"Missing metric {name}: {snapshots[phase]}")

        expected = {
            "http_requests_total": 9,
            "websocket_upgrades_total": 2,
            "tcp_flows_total": 6,
            "tcp_established_total": 5,
            "udp_flows_total": 1,
            "stream_gaps_total": 0,
            "websocket_connections": 1,
            "tcp_connections": 1,
            "udp_flows_active": 1,
        }
        for name, value in expected.items():
            assert metric("SNAPSHOT", name) == value, (
                name,
                metric("SNAPSHOT", name),
                value,
            )
        assert (
            metric("FINAL", "http_requests_total", "10.183.102.2", "10.183.101.2") == 1
        )
        for name in ("websocket_connections", "tcp_connections", "udp_flows_active"):
            assert metric("FINAL", name) == 0, (name, metric("FINAL", name))
        assert not result.stderr, result.stderr
        # Advancing capture time expires idle flows before process shutdown.
        fixture().write(work / "game.pcap", age_seconds=360)
        service = render.game_metrics(config)["router/game-metrics.service"]
        arguments = shlex.split(
            next(
                line.removeprefix("ExecStart=")
                for line in service.splitlines()
                if line.startswith("ExecStart=")
            )
        )
        capture_filter = arguments[arguments.index("-f") + 1]
        command[-1] += " -f " + shlex.quote(capture_filter)
        aged = container(
            command, check=True, capture_output=True, text=True, timeout=60
        )
        snapshots["AGED"] = json.loads(
            next(
                line.split(" ", 1)[1]
                for line in aged.stdout.splitlines()
                if line.startswith("SNAPSHOT ")
            )
        )
        for name in ("websocket_connections", "tcp_connections", "udp_flows_active"):
            assert metric("AGED", name) == 0, (name, metric("AGED", name))
        assert metric("AGED", "http_requests_total") == expected["http_requests_total"]
        assert not aged.stderr, aged.stderr
        print(
            "Game metrics passed: HTTP reassembly/retransmission/pipelining, WS upgrade/reset/expiry, TCP/UDP, team direction and excluded sources."
        )
        (work / "check_endpoint.py").write_text(
            (ROOT / "tests/check_game_endpoint.py").read_text()
        )
        container(
            [
                "docker",
                "run",
                "--rm",
                "--network",
                "none",
                "--read-only",
                "--cap-drop",
                "ALL",
                "--cap-add",
                "NET_RAW",
                "--security-opt",
                "no-new-privileges",
                "--workdir",
                "/work",
                "--mount",
                f"type=bind,src={work},dst=/work,readonly",
                "--entrypoint",
                "/bin/sh",
                IMAGE,
                "-c",
                "pwd -P && exec python3 -B check_endpoint.py",
            ],
            check=True,
            timeout=30,
        )

    container(
        [
            "docker",
            "run",
            "--rm",
            "--network",
            "none",
            "--read-only",
            "--cap-drop",
            "ALL",
            "--workdir",
            "/work",
            "--tmpfs",
            "/work/tmp:mode=1777",
            "--env",
            "TMPDIR=/work/tmp",
            "--mount",
            f"type=bind,src={ROOT.parent / 'deployment/monitoring/rules'},dst=/work/rules,readonly",
            "--mount",
            f"type=bind,src={ROOT / 'tests/game_rules.yml'},dst=/work/game_rules.yml,readonly",
            "--entrypoint",
            "/bin/sh",
            "prom/prometheus:v3.15.0",
            "-c",
            "pwd -P && promtool check rules /work/rules/game.yml && promtool test rules /work/game_rules.yml",
        ],
        check=True,
        timeout=60,
    )


if __name__ == "__main__":
    os.umask(0o077)
    main()

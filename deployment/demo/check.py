"""Exercise the running local demo, restoring its network phase on exit."""

import json
import os
import subprocess
import time
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
COMPOSE = [
    "docker",
    "compose",
    "--project-directory",
    str(ROOT / "deployment/docker"),
    "--env-file",
    str(ROOT / "config/runtime/demo.env"),
    "-f",
    str(ROOT / "deployment/docker/compose.yml"),
    "-f",
    str(ROOT / "deployment/docker/compose.monitoring.yml"),
    "-f",
    str(ROOT / "deployment/docker/compose.demo.yml"),
    "--profile",
    "monitoring",
]
SEEN = set()
PASSED = 0


def execute(role, *command, check=True, **kwargs):
    if role not in SEEN:
        location = subprocess.run(
            COMPOSE + ["exec", "-T", role, "pwd", "-P"],
            capture_output=True,
            text=True,
            check=True,
            timeout=10,
        )
        if location.stdout.strip() != "/app":
            raise RuntimeError(f"Unexpected working directory for {role}")
        SEEN.add(role)
    return subprocess.run(
        COMPOSE + ["exec", "-T", role, *command],
        check=check,
        capture_output=True,
        text=True,
        timeout=15,
        **kwargs,
    )


def passed(description):
    global PASSED
    PASSED += 1
    print(f"PASS {description}", flush=True)


def probe(role, host, kind="http", port=8080, allowed=True, peer=None, path="/health"):
    result = execute(
        role,
        "python",
        "probe.py",
        kind,
        host,
        "--port",
        str(port),
        "--path",
        path,
        check=False,
    )
    document = json.loads(result.stdout)
    if allowed:
        assert result.returncode == 0, (role, host, port, result.stdout, result.stderr)
        if peer is not None:
            assert document.get("peer") == peer, document
    else:
        assert result.returncode == 1 and document == {"connected": False}, (
            role,
            host,
            port,
            result.stdout,
            result.stderr,
        )
    passed(
        f"{role} -> {host}:{port} {'allowed' if allowed else 'blocked'}"
        + (f", source {peer}" if peer else "")
    )


def phase(value):
    execute("demo-router", "nft", "-f", f"/etc/forcad/router/{value}.nft")


def query(expression):
    url = "http://127.0.0.1:9090/api/v1/query?" + urllib.parse.urlencode(
        {"query": expression}
    )
    with urllib.request.urlopen(url, timeout=5) as response:
        document = json.load(response)
    assert document["status"] == "success", document
    return document["data"]["result"]


def value(expression):
    rows = query(expression)
    return float(rows[0]["value"][1]) if rows else 0


def wait_for(description, predicate, seconds=75):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if predicate():
            passed(description)
            return
        time.sleep(2)
    raise AssertionError("Timed out: " + description)


def check_routes():
    for client in ("demo-player1", "demo-player2", "demo-admin"):
        game = execute(client, "ip", "route", "get", "10.183.102.2").stdout
        local = execute(client, "ip", "route", "get", "10.0.2.15").stdout
        assert "dev wg0" in game, game
        assert "dev wg0" not in local, local
        passed(client + " routes only the game prefix through WireGuard")


def main():
    check_routes()
    original = (ROOT / "config/runtime/demo/phase").read_text().strip()
    assert original in {"game", "pre-game"}
    filtering = False
    try:
        phase("game")
        for player, target, gateway in (
            ("demo-player1", "10.183.101.2", "10.183.101.1"),
            ("demo-player1", "10.183.102.2", "10.183.102.1"),
            ("demo-player2", "10.183.101.2", "10.183.101.1"),
            ("demo-admin", "10.183.102.2", "10.183.102.1"),
            ("demo-forcad", "10.183.102.2", "10.183.102.1"),
        ):
            probe(player, target, peer=gateway)
        probe("demo-player1", "10.183.102.2", "ws", 8081)
        probe("demo-player1", "10.183.102.2", "tcp", 9001)
        probe("demo-player1", "10.183.102.2", "udp", 9000)
        probe("demo-player1", "10.183.101.2", "tcp", 22)
        probe("demo-player1", "10.183.102.2", "tcp", 22, allowed=False)
        probe("demo-player2", "10.183.101.2", "tcp", 22, allowed=False)
        probe("demo-admin", "10.183.102.2", "tcp", 22)
        probe("demo-player1", "10.183.2.10", port=80, path="/api/client/teams/")
        for host, port in (
            ("10.183.2.10", 9090),
            ("10.183.2.1", 9109),
            ("10.183.2.1", 9911),
            ("10.183.1.2", 22),
        ):
            probe("demo-player1", host, "tcp", port, allowed=False)
        probe("demo-team1", "10.183.102.2", allowed=False)
        probe("demo-team2", "10.183.101.2", allowed=False)
        result = execute(
            "demo-player1", "ping", "-c", "1", "-W", "1", "10.183.200.9", check=False
        )
        assert result.returncode == 1, result.stderr
        passed("VPN peers from different teams are isolated")

        phase("pre-game")
        for target in ("10.183.101.2", "10.183.102.2"):
            execute("demo-player1", "ping", "-c", "1", "-W", "2", target)
            passed("pre-game ping " + target)
            probe("demo-player1", target, allowed=False)
            probe("demo-player1", target, "tcp", 22, allowed=False)
        probe("demo-admin", "10.183.102.2", "tcp", 22)
        probe("demo-forcad", "10.183.102.2", peer="10.183.102.1")
        probe("demo-player1", "10.183.2.10", port=80, path="/api/client/teams/")
        phase("game")

        existing = execute(
            "demo-team2",
            "nft",
            "list",
            "table",
            "inet",
            "demo_filter_test",
            check=False,
        )
        assert existing.returncode != 0, (
            "Refusing to replace an existing guest test table"
        )
        execute(
            "demo-team2",
            "nft",
            "-f",
            "-",
            input=(
                "table inet demo_filter_test {\n chain input {\n"
                "type filter hook input priority -20; policy accept;\n tcp dport 9001 drop\n }\n}\n"
            ),
        )
        filtering = True
        probe("demo-player1", "10.183.102.2", "tcp", 9001, allowed=False)
        probe("demo-player1", "10.183.102.2", peer="10.183.102.1")
        execute("demo-team2", "nft", "delete", "table", "inet", "demo_filter_test")
        filtering = False
        probe("demo-player1", "10.183.102.2", "tcp", 9001)
        passed("vulnbox filtering is controlled independently by the team")

        wait_for(
            "game sensor is healthy",
            lambda: (
                value('up{job="forcad_game_sensor"}') == 1
                and bool(query("forcad:game_sensor_ready"))
            ),
        )
        wait_for(
            "both HTTP directions reach Prometheus",
            lambda: all(
                value(
                    f'forcad_game_http_requests_total{{source_team_ip="{source}",team_ip="{target}"}}'
                )
                > 0
                for source, target in (
                    ("10.183.101.2", "10.183.102.2"),
                    ("10.183.102.2", "10.183.101.2"),
                )
            ),
        )
        wait_for(
            "both teams have an observed WebSocket",
            lambda: all(
                value(
                    f'forcad_game_websocket_connections{{source_team_ip="{source}",team_ip="{target}"}}'
                )
                >= 1
                for source, target in (
                    ("10.183.101.2", "10.183.102.2"),
                    ("10.183.102.2", "10.183.101.2"),
                )
            ),
        )
        assert value("sum(forcad_game_udp_flows_total)") > 0
        assert value("sum(forcad_game_tcp_established_total)") > 0
        passed("TCP and UDP metrics reach Prometheus")
        dashboard = json.loads(
            (ROOT / "deployment/monitoring/grafana/dashboards/game.json").read_text()
        )
        panels = list(dashboard["panels"])
        for panel in panels:
            panels.extend(panel.get("panels", []))
            for target in panel.get("targets", []):
                expression = (
                    target.get("expr", "")
                    .replace("$__rate_interval", "1m")
                    .replace("${source}", ".*")
                    .replace("${target}", ".*")
                )
                if expression:
                    rows = query(expression)
                    if panel["type"] == "timeseries":
                        assert len(rows) <= 1, (panel["title"], rows)
        pairs = query(
            "sum by (source_team_ip, team_ip) (rate(forcad_network_source_packets_total[1m]) * on (source_ip) group_left (source_team_ip) forcad:game_peer_info)"
        )
        assert any(
            row["metric"]
            == {"source_team_ip": "10.183.101.2", "team_ip": "10.183.102.2"}
            and float(row["value"][1]) > 0
            for row in pairs
        ), pairs
        passed("dashboard queries and VPN source -> team packet attribution")

        selector = '{source_team_ip="10.183.101.2",team_ip="10.183.102.2"}'
        before = value("forcad_game_http_requests_total" + selector)
        before_ws = value("forcad_game_websocket_connections" + selector)
        load = subprocess.Popen(
            COMPOSE
            + [
                "exec",
                "-T",
                "demo-farm1",
                "python",
                "load.py",
                "--rps",
                "20",
                "--seconds",
                "35",
                "--ws",
                "3",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        try:
            wait_for(
                "load adds three observed WebSockets",
                lambda: (
                    value("forcad_game_websocket_connections" + selector)
                    >= before_ws + 3
                ),
                seconds=30,
            )
            output, error = load.communicate(timeout=45)
            assert load.returncode == 0, error
            report = json.loads(output)
            assert report["requests"] >= 400 and report["failed"] == 0, report
            wait_for(
                "game HTTP counter includes the load",
                lambda: (
                    value("forcad_game_http_requests_total" + selector) >= before + 400
                ),
            )
            wait_for(
                "closed load WebSockets are removed",
                lambda: (
                    value("forcad_game_websocket_connections" + selector) <= before_ws
                ),
            )
            passed(
                f"bounded load: {report['requests']} successful HTTP requests; WS open/close counted"
            )
        finally:
            if load.poll() is None:
                load.terminate()
                load.communicate(timeout=10)
    finally:
        if filtering:
            execute("demo-team2", "nft", "delete", "table", "inet", "demo_filter_test")
        phase(original)
    print(
        f"Demo checks passed: {PASSED}. Network phase restored to {original}.",
        flush=True,
    )


if __name__ == "__main__":
    os.umask(0o077)
    main()

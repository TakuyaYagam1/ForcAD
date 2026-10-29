#!/usr/bin/env python3
"""Check dashboard provisioning and PromQL without changing an existing stack."""

import json
import os
import subprocess
import tempfile
import time
import urllib.parse
from pathlib import Path

from dashboard_cases import build_cases, build_infrastructure_cases

ROOT = Path(__file__).resolve().parents[2]
MONITORING = ROOT / "deployment/monitoring"


def run(*args):
    return subprocess.run(
        args, check=True, text=True, capture_output=True, timeout=60
    ).stdout.strip()


def main():
    containers = []
    with tempfile.TemporaryDirectory(prefix="forcad-dashboards-") as directory:
        work = Path(directory)
        # The same config/rules and dashboards, with paths rooted in the test workdir.
        (work / "prometheus.yml").write_text(
            (MONITORING / "prometheus.yml")
            .read_text()
            .replace("/etc/prometheus", "/work/monitoring")
        )
        provisioning = work / "provisioning"
        (provisioning / "dashboards").mkdir(parents=True)
        (provisioning / "dashboards/dashboards.yml").write_text(
            (MONITORING / "grafana/provisioning/dashboards/dashboards.yml")
            .read_text()
            .replace(
                "/var/lib/grafana/dashboards", "/work/monitoring/grafana/dashboards"
            )
        )
        # No external datasource is contacted during provisioning.
        (provisioning / "datasources").mkdir()
        (provisioning / "datasources/prometheus.yml").write_text(
            "apiVersion: 1\ndatasources:\n  - name: Prometheus\n    uid: prometheus\n    type: prometheus\n    access: proxy\n    url: http://127.0.0.1:9090\n"
        )
        # Containers use non-root users and only need to read these public configs.
        for path in work.rglob("*"):
            path.chmod(0o755 if path.is_dir() else 0o644)
        try:
            prometheus = run(
                "docker",
                "run",
                "-d",
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
                "--tmpfs",
                "/work/data:uid=65534,gid=65534,mode=0700",
                "--mount",
                f"type=bind,src={MONITORING},dst=/work/monitoring,readonly",
                "--mount",
                f"type=bind,src={work / 'prometheus.yml'},dst=/work/prometheus.yml,readonly",
                "--entrypoint",
                "/bin/sh",
                "prom/prometheus:v3.15.0",
                "-c",
                "pwd -P && promtool check config /work/prometheus.yml && exec prometheus --config.file=/work/prometheus.yml --storage.tsdb.path=/work/data --web.listen-address=127.0.0.1:9090",
            )
            containers.append(prometheus)
            game = json.loads((MONITORING / "grafana/dashboards/game.json").read_text())
            infrastructure = json.loads(
                (MONITORING / "grafana/dashboards/infrastructure.json").read_text()
            )
            cases = build_cases(game)
            cases["tests"].extend(build_infrastructure_cases(infrastructure)["tests"])
            result = subprocess.run(
                [
                    "docker",
                    "exec",
                    "-i",
                    "-e",
                    "TMPDIR=/work/data",
                    prometheus,
                    "promtool",
                    "test",
                    "rules",
                    "/dev/stdin",
                ],
                input=json.dumps(cases),
                text=True,
                capture_output=True,
                check=False,
                timeout=60,
            )
            print(result.stdout.strip())
            if result.returncode:
                raise AssertionError(result.stderr)
            grafana = run(
                "docker",
                "run",
                "-d",
                "--rm",
                "--network",
                f"container:{prometheus}",
                "--read-only",
                "--cap-drop",
                "ALL",
                "--security-opt",
                "no-new-privileges",
                "--workdir",
                "/work",
                "--tmpfs",
                "/work/data:uid=472,gid=0,mode=0700",
                "--mount",
                f"type=bind,src={MONITORING},dst=/work/monitoring,readonly",
                "--mount",
                f"type=bind,src={provisioning},dst=/work/provisioning,readonly",
                "--env",
                "GF_PATHS_DATA=/work/data",
                "--env",
                "GF_PATHS_LOGS=/work/data/logs",
                "--env",
                "GF_PATHS_PLUGINS=/work/data/plugins",
                "--env",
                "GF_PATHS_PROVISIONING=/work/provisioning",
                "--env",
                "GF_SERVER_HTTP_ADDR=127.0.0.1",
                "--env",
                "GF_AUTH_ANONYMOUS_ENABLED=true",
                "--env",
                "GF_ANALYTICS_REPORTING_ENABLED=false",
                "--env",
                "GF_ANALYTICS_CHECK_FOR_UPDATES=false",
                "--env",
                "GF_PLUGINS_PREINSTALL_DISABLED=true",
                "--entrypoint",
                "/bin/sh",
                "grafana/grafana:13.2.2",
                "-c",
                "pwd -P && exec /run.sh",
            )
            containers.append(grafana)

            def get(url):
                return json.loads(
                    run("docker", "exec", prometheus, "wget", "-qO-", url)
                )

            deadline = time.monotonic() + 45
            while time.monotonic() < deadline:
                try:
                    get("http://127.0.0.1:3000/api/dashboards/uid/forcad-game-traffic")
                    break
                except subprocess.CalledProcessError:
                    time.sleep(0.5)
            else:
                raise AssertionError("Grafana did not provision the game dashboard")
            queries = 0
            dashboards = list((MONITORING / "grafana/dashboards").glob("*.json"))
            for path in dashboards:
                dashboard = json.loads(path.read_text())
                loaded = get(
                    "http://127.0.0.1:3000/api/dashboards/uid/" + dashboard["uid"]
                )
                assert loaded["dashboard"]["title"] == dashboard["title"]
                panels = list(dashboard["panels"])
                for panel in panels:
                    panels.extend(panel.get("panels", []))
                    for target in panel.get("targets", []):
                        expression = target.get("expr", "")
                        if not expression:
                            continue
                        for name, value in {
                            "$__rate_interval": "1m",
                            "$__interval": "15s",
                            "$__range": "1h",
                            "${team}": ".*",
                            "${team:regex}": ".*",
                            "${service:regex}": ".*",
                            "${source}": ".*",
                            "${target}": ".*",
                            "${component}": ".*",
                            "${instance}": ".*",
                            "$team": ".*",
                        }.items():
                            expression = expression.replace(name, value)
                        response = get(
                            "http://127.0.0.1:9090/api/v1/query?"
                            + urllib.parse.urlencode({"query": expression})
                        )
                        assert response["status"] == "success", (
                            path.name,
                            panel["title"],
                            response,
                        )
                        queries += 1
            print(
                f"Provisioned {len(dashboards)} dashboards and evaluated {queries} PromQL queries."
            )
        except BaseException:
            for container in containers:
                print(run("docker", "logs", "--tail", "20", container))
            raise
        finally:
            for container in reversed(containers):
                subprocess.run(
                    ["docker", "rm", "-f", container],
                    check=False,
                    capture_output=True,
                    timeout=20,
                )


if __name__ == "__main__":
    os.umask(0o077)
    main()

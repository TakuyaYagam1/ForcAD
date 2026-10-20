"""Plan and check ForcAD application host files."""

import json
import subprocess
import time
from pathlib import Path

from hostlib.base import BundleBase
from hostlib.cli import main as run_cli
from hostlib.common import (
    dump_yaml,
    merge_env,
    merge_targets,
    merge_teams,
    read_yaml,
    regular,
    run,
)

HEALTH_CHECK_TIMEOUT = 120
HEALTH_CHECK_INTERVAL = 5


class Bundle(BundleBase):
    ROLE = "forcad"

    def plan_role(self, repo, wireguard_dir, network_metrics, game_metrics):
        self.plan_forcad(repo, network_metrics, game_metrics)

    def plan_forcad(self, repo, network_metrics, game_metrics):
        if not repo or not Path(repo).is_absolute():
            raise ValueError("Use --repo /absolute/path/to/ForcAD")
        project = self.path(repo)
        if not (project / "control.sh").is_file():
            raise ValueError("--repo must point to an existing ForcAD checkout")
        config = project / "config/config.yml"
        document = read_yaml(config)
        original = json.dumps(document, sort_keys=True, default=str)
        merge_teams(document, self.config["teams"])
        data = (
            config.read_bytes()
            if original == json.dumps(document, sort_keys=True, default=str)
            else dump_yaml(document)
        )
        self.put(f"{repo}/config/config.yml", data)
        env = project / "deployment/docker/.env"
        updates = dict(
            line.split("=", 1)
            for line in (self.directory / "environment.env").read_text().splitlines()
            if line and not line.startswith("#")
        )
        self.put(
            f"{repo}/deployment/docker/.env",
            merge_env(regular(env).read_text() if env.exists() else "", updates),
        )
        for enabled, name, port in (
            (network_metrics, "routers.yml", 9109),
            (game_metrics, "game-sensors.yml", 9911),
        ):
            if enabled:
                path = project / "deployment/monitoring/file_sd" / name
                groups = read_yaml(path) if path.exists() else []
                self.put(
                    f"{repo}/deployment/monitoring/file_sd/{name}",
                    dump_yaml(
                        merge_targets(
                            groups, f"{self.config['checks']['metrics_address']}:{port}"
                        )
                    ),
                    0o644,
                )

    def validate_role(self, work):
        repo = Path(self.options["repo"])
        binary = repo / "backend/bin/forcad"
        if not binary.is_file():
            raise ValueError("Build ForcAD first: make -C backend build")
        run(
            str(binary),
            "control",
            "--project-root",
            str(repo),
            "validate",
            "--full",
        )
        path = work / "config.yml"
        path.write_bytes(self.changes[self.path(f"{repo}/config/config.yml")][0])
        path.chmod(0o600)
        run(
            str(binary),
            "control",
            "--project-root",
            str(repo),
            "validate",
            "--full",
            "--config",
            str(path),
        )

    def test_live_role(self):
        repo = Path(self.options["repo"])
        binary = str(repo / "backend/bin/forcad")
        run(binary, "control", "--project-root", str(repo), "validate", "--full")
        command = (
            "monitoring"
            if self.options["network_metrics"] or self.options["game_metrics"]
            else "compose"
        )
        expected = {"api", "receiver", "worker", "ticker", "nginx"}
        if command == "monitoring":
            expected |= {"prometheus", "grafana"}
        self.wait_for_containers(binary, repo, command, expected)
        self.http(self.config["checks"]["forcad_url"])

    def wait_for_containers(self, binary, repo, command, expected):
        deadline = time.monotonic() + HEALTH_CHECK_TIMEOUT
        while True:
            output = run(
                binary,
                "control",
                "--project-root",
                str(repo),
                command,
                "ps",
                "--all",
                "--format",
                "json",
            )
            containers = (
                json.loads(output)
                if output.lstrip().startswith("[")
                else [json.loads(line) for line in output.splitlines() if line.strip()]
            )
            services = {container.get("Service") for container in containers}
            missing = expected - services
            pending = []
            failed = []

            for container in containers:
                service = container.get("Service", "unknown")
                state = container.get("State")
                health = container.get("Health")
                if (
                    service in {"initializer", "config-loader", "postgres-data"}
                    and state == "exited"
                    and container.get("ExitCode") == 0
                ):
                    continue
                if state == "running" and health in (None, "", "healthy"):
                    continue
                if state in {"created", "restarting"} or (
                    state == "running" and health == "starting"
                ):
                    pending.append(f"{service} (state={state}, health={health})")
                    continue
                failed.append(f"{service} (state={state}, health={health})")

            if not missing and not pending and not failed:
                return
            if failed or time.monotonic() >= deadline:
                details = []
                if missing:
                    details.append(f"missing services: {', '.join(sorted(missing))}")
                if pending:
                    details.append(f"still starting: {', '.join(pending)}")
                if failed:
                    details.append(f"failed services: {', '.join(failed)}")
                diagnostics = self.container_logs(
                    binary,
                    repo,
                    command,
                    {item.split(" (", 1)[0] for item in pending + failed},
                )
                suffix = f"\n{diagnostics}" if diagnostics else ""
                if failed:
                    message = f"ForcAD containers failed readiness check: {'; '.join(details)}"
                else:
                    message = (
                        f"ForcAD containers did not become healthy within "
                        f"{HEALTH_CHECK_TIMEOUT}s: {'; '.join(details)}"
                    )
                raise ValueError(message + suffix)
            time.sleep(min(HEALTH_CHECK_INTERVAL, max(0, deadline - time.monotonic())))

    @staticmethod
    def container_logs(binary, repo, command, services):
        outputs = []
        for service in sorted(services):
            try:
                logs = run(
                    binary,
                    "control",
                    "--project-root",
                    str(repo),
                    command,
                    "logs",
                    "--no-color",
                    "--tail",
                    "80",
                    service,
                )
            except (OSError, ValueError, subprocess.SubprocessError) as error:
                logs = f"Could not read logs: {error}"
            if logs.strip():
                outputs.append(f"Logs for {service}:\n{logs.rstrip()}")
        return "\n".join(outputs)


def main():
    run_cli(Bundle, Path(__file__).resolve().parent)


if __name__ == "__main__":
    main()

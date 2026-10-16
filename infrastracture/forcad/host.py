"""Plan and check ForcAD application host files."""

import json
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
                    f"{repo}/config/runtime/network/{name}",
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
        expected = {"api", "receiver", "worker", "ticker", "nginx"}
        if command == "monitoring":
            expected |= {"prometheus", "grafana"}
        if not expected <= {container.get("Service") for container in containers}:
            raise ValueError("Required ForcAD containers are missing")
        for container in containers:
            if (
                container.get("Service")
                in {"initializer", "config-loader", "postgres-data"}
                and container.get("State") == "exited"
                and container.get("ExitCode") == 0
            ):
                continue
            if container.get("State") != "running" or container.get("Health") not in (
                None,
                "",
                "healthy",
            ):
                raise ValueError(
                    f"Container is not healthy: {container.get('Service')}"
                )
        self.http(self.config["checks"]["forcad_url"])


def main():
    run_cli(Bundle, Path(__file__).resolve().parent)


if __name__ == "__main__":
    main()

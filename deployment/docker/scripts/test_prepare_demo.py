"""Check demo expansion without touching a running database."""

import contextlib
import io
import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import demo_routes
import prepare_demo
import yaml

ROOT = Path(__file__).resolve().parents[3]


class PrepareDemoTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        for relative in (
            "deployment/demo/settings.toml",
            "deployment/docker/compose.demo.yml",
            "deployment/monitoring/prometheus.yml",
        ):
            destination = self.root / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, destination)
        (self.root / "config").mkdir()
        self.settings = self.root / "deployment/demo/settings.toml"
        self.full_settings = self.settings.read_text()
        self.config = self.root / ".demo/config/config.yml"
        self.output = self.root / ".demo/network"

    def two_teams(self):
        start = self.full_settings.index('[[teams.entries]]\nname = "daywave"')
        end = self.full_settings.index("[env]", start)
        self.settings.write_text(self.full_settings[:start] + self.full_settings[end:])

    def prepare(self):
        with (
            patch.object(sys, "argv", ["prepare_demo.py", str(self.root)]),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            prepare_demo.main()

    @unittest.skipUnless(shutil.which("docker"), "Docker Compose is not installed")
    def test_rendered_demo_owns_all_credentials_storage_and_published_ports(self):
        self.two_teams()
        self.prepare()
        docker_dir = self.root / "deployment/docker"
        (docker_dir / "scripts").mkdir()
        for name in ("compose.yml", "compose.monitoring.yml", "scripts/demo.sh"):
            shutil.copyfile(ROOT / "deployment/docker" / name, docker_dir / name)
        script = docker_dir / "scripts/demo.sh"
        args = ["bash", script, "config", "--format", "json", "--no-env-resolution"]
        env = {
            "HOME": str(self.root),
            "PATH": os.environ["PATH"],
            "COMPOSE_PROJECT_NAME": "forcad",
            "FORCAD_RUNTIME_DIR": str(self.root / "config/runtime"),
        }
        model = json.loads(subprocess.check_output(args, env=env))
        state = self.root / ".demo"
        self.assertEqual(model["name"], "forcad-demo")
        for section in ("networks", "volumes"):
            for resource in model[section].values():
                self.assertTrue(resource["name"].startswith("forcad-demo_"))
        ports = set()
        for service in model["services"].values():
            for env in service.get("env_file", []):
                self.assertTrue(Path(env["path"]).is_relative_to(state))
            for mount in service.get("volumes", []):
                if mount["type"] == "bind":
                    self.assertTrue(mount.get("read_only"))
                    self.assertFalse(
                        Path(mount["source"]).is_relative_to(self.root / "config")
                    )
            for binding in service.get("ports", []):
                self.assertEqual(binding["host_ip"], "127.0.0.1")
                ports.add(int(binding["published"]))
        self.assertEqual(ports, {18080, 19090, 13000})
        mounts = model["services"]["config-loader"]["volumes"]
        config = next(m for m in mounts if m["target"] == "/source/config.yml")
        self.assertEqual(Path(config["source"]), state / "config/runtime/game.yml")

    def test_overlapping_host_route_is_not_exempted_by_an_existing_demo_bridge(self):
        nets = {"team": {"ipam": {"config": [{"subnet": "10.183.101.0/24"}]}}}
        own = {"br-demo"}
        routes = [
            {"dst": "default", "dev": "eth0"},
            {"dst": "10.183.101.0/24", "dev": "br-demo"},
        ]
        demo_routes.check_routes(nets, routes, own)
        routes.append({"dst": "10.183.0.0/16", "dev": "wg0"})
        with self.assertRaisesRegex(SystemExit, "10.183.0.0/16.*wg0"):
            demo_routes.check_routes(nets, routes, own)

    def test_demo_ignores_tournament_and_legacy_state(self):
        production = self.root / "config/config.yml"
        credentials = self.root / "config/runtime/admin.env"
        legacy = self.root / "config/runtime/demo/settings.toml"
        legacy.parent.mkdir(parents=True)
        production.write_text("tournament configuration sentinel")
        credentials.write_text("tournament credential sentinel")
        legacy.write_text("legacy demo sentinel")
        self.prepare()
        self.assertEqual(production.read_text(), "tournament configuration sentinel")
        self.assertEqual(credentials.read_text(), "tournament credential sentinel")
        self.assertEqual(legacy.read_text(), "legacy demo sentinel")
        self.assertTrue(self.config.is_file())

    def test_fresh_demo_uses_settings_for_game_and_teams(self):
        self.prepare()
        settings = prepare_demo.render.load_config(self.settings)
        expected = yaml.safe_load(prepare_demo.render.forcad_config(settings))
        actual = yaml.safe_load(self.config.read_text())
        self.assertEqual(actual["game"], expected["game"])
        self.assertEqual(actual["tasks"], expected["tasks"])
        self.assertEqual(
            [t["name"] for t in actual["teams"]], [t["name"] for t in expected["teams"]]
        )
        team_count = len(expected["teams"])
        self.assertEqual(len({t["token"] for t in actual["teams"]}), team_count)
        self.assertEqual(
            [(task["puts"], task["gets"]) for task in actual["tasks"]],
            [(2, 2), (0, 0), (0, 0), (0, 0), (0, 0)],
        )
        compose = yaml.safe_load((self.output / "compose.yml").read_text())
        sensor = compose["services"]["demo-sensor"]["command"]
        service = (self.output / "router/game-metrics.service").read_text()
        start = next(line for line in service.splitlines() if line.startswith("ExecStart="))
        router = shlex.split(start.removeprefix("ExecStart="))
        for arg in ("-f", "-i"):
            self.assertEqual(
                sensor[sensor.index(arg) + 1], router[router.index(arg) + 1]
            )
        for number in range(1, team_count + 1):
            self.assertIn(f"demo-team{number}", compose["networks"])
            self.assertIn(f"demo-farm{number}", compose["services"])
            self.assertTrue((self.output / f"players/team{number}.conf").is_file())
        last_farm = compose["services"][f"demo-farm{team_count}"]
        self.assertIn("demo-team1", last_farm["depends_on"])
        self.assertIn(
            f"DEMO_PEER={actual['teams'][0]['ip']}",
            (self.output / f"team{team_count}.env").read_text(),
        )

    def test_existing_database_rejects_game_config_drift(self):
        self.prepare()
        document = yaml.safe_load(self.config.read_text())
        document["game"]["round_time"] += 1
        self.config.write_text(yaml.safe_dump(document))
        original = self.config.read_bytes()
        with self.assertRaisesRegex(SystemExit, "differs from settings.toml"):
            self.prepare()
        self.assertEqual(self.config.read_bytes(), original)

    def test_append_keeps_existing_tokens_keys_and_is_idempotent(self):
        self.two_teams()
        self.prepare()
        before = yaml.safe_load(self.config.read_text())
        wireguard = self.output / "wireguard"
        server = (wireguard / "server/wg-attack.conf").read_text()
        private = (
            (self.output / "players/team1.conf").read_text().split("AllowedIPs")[0]
        )
        admin = (wireguard / "server/wg-admin.conf").read_bytes()
        self.settings.write_text(self.full_settings)
        self.prepare()
        after = yaml.safe_load(self.config.read_text())
        self.assertEqual(before["teams"], after["teams"][:2])
        self.assertEqual(before["game"], after["game"])
        self.assertTrue(
            (wireguard / "server/wg-attack.conf").read_text().startswith(server)
        )
        self.assertEqual(admin, (wireguard / "server/wg-admin.conf").read_bytes())
        self.assertTrue(
            (self.output / "players/team1.conf").read_text().startswith(private)
        )
        files = {p: p.read_bytes() for p in wireguard.rglob("*") if p.is_file()}
        config = self.config.read_bytes()
        self.prepare()
        self.assertEqual(config, self.config.read_bytes())
        for path, content in files.items():
            self.assertEqual(content, path.read_bytes())

    def test_reordering_existing_teams_is_rejected_before_writes(self):
        self.prepare()
        original = self.config.read_bytes()
        self.settings.write_text(
            self.full_settings.replace('name = "daywave"', 'name = "renamed"')
        )
        with self.assertRaisesRegex(SystemExit, "only append teams"):
            self.prepare()
        self.assertEqual(original, self.config.read_bytes())

    def test_old_inventories_migrate_without_rotating_keys(self):
        self.two_teams()
        self.prepare()
        document = yaml.safe_load(self.config.read_text())
        tokens = [t["token"] for t in document["teams"]]
        for team, address in zip(document["teams"], prepare_demo.OLD_IPS[1]):
            team["ip"] = address
        self.config.write_text(yaml.safe_dump(document))
        wireguard = self.output / "wireguard"
        for path in [*wireguard.rglob("*.conf"), wireguard / "assignments.csv"]:
            path.write_text(
                path.read_text()
                .replace("10.183.200.", "10.200.0.")
                .replace("10.183.201.", "10.201.0.")
            )
        server = wireguard / "server/wg-attack.conf"
        original = server.read_bytes()
        self.prepare()
        actual = yaml.safe_load(self.config.read_text())["teams"]
        self.assertEqual([t["token"] for t in actual], tokens)
        self.assertEqual([t["ip"] for t in actual], ["10.183.101.2", "10.183.102.2"])
        self.assertEqual(
            original,
            (
                self.output / "wireguard-before-network/server/wg-attack.conf"
            ).read_bytes(),
        )
        self.assertEqual(
            server.read_bytes(), original.replace(b"10.200.0.", b"10.183.200.")
        )


if __name__ == "__main__":
    unittest.main()

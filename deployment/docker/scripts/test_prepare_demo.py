"""Check demo expansion without touching a running database."""

import contextlib
import io
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

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
        self.config = self.root / "config/config.yml"
        self.output = self.root / "config/runtime/demo"

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
        self.assertEqual(len({t["token"] for t in actual["teams"]}), 10)
        self.assertEqual(
            [(task["puts"], task["gets"]) for task in actual["tasks"]],
            [(2, 2), (0, 0), (0, 0), (0, 0), (0, 0)],
        )
        compose = yaml.safe_load((self.output / "compose.yml").read_text())
        for number in range(1, 11):
            self.assertIn(f"demo-team{number}", compose["networks"])
            self.assertIn(f"demo-farm{number}", compose["services"])
            self.assertTrue((self.output / f"players/team{number}.conf").is_file())
        self.assertIn("demo-team1", compose["services"]["demo-farm10"]["depends_on"])
        self.assertIn(
            "DEMO_PEER=10.183.101.2",
            (self.root / "config/runtime/demo-team10.env").read_text(),
        )

    def test_existing_database_rejects_game_config_drift(self):
        self.prepare()
        document = yaml.safe_load(self.config.read_text())
        document["game"]["round_time"] += 1
        self.config.write_text(yaml.safe_dump(document))
        original = self.config.read_bytes()
        (self.root / "docker_volumes/postgres/data").mkdir(parents=True)
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
        (self.root / "docker_volumes/postgres/data").mkdir(parents=True)
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

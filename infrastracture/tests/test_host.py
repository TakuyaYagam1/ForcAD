import base64
import copy
import importlib.util
import json
import stat
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

ROOT = Path(__file__).resolve().parents[1]
from helpers import checks, common, files, small_config, source_bundle

import render


class HostTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.work = Path(self.scratch.name)
        self.config = small_config(render.load_config(ROOT / "settings.example.toml"))
        self.files = render.render(self.config)
        self.bundle = self.work / "bundle"
        render.write_bundle(self.bundle, self.files)
        self.root = self.work / "host"
        self.root.mkdir()

    def make(self, role):
        return source_bundle(role, self.bundle / role, self.root)

    def write(self, path, data, mode=0o640):
        target = self.root / path.lstrip("/")
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(data)
        target.chmod(mode)
        return target

    def test_install_is_idempotent_and_keeps_a_private_backup(self):
        old = self.write("/etc/network/interfaces", "auto lo\niface lo inet loopback\n")
        installer = self.make("router")
        installer.plan()
        with patch.object(
            common, "run", side_effect=AssertionError("install must not run commands")
        ):
            installer.install()
            backups = list((self.root / "var/backups/forcad/router").iterdir())
            self.assertEqual(len(backups), 1)
            backup = backups[0] / "etc/network/interfaces"
            self.assertEqual(backup.read_text(), "auto lo\niface lo inet loopback\n")
            self.assertEqual(stat.S_IMODE(backup.stat().st_mode), 0o600)
            before = old.stat().st_mtime_ns
            repeated = self.make("router")
            repeated.plan()
            repeated.install()
            self.assertEqual(old.stat().st_mtime_ns, before)
            self.assertEqual(len(list(backups[0].parent.iterdir())), 1)
        self.assertIn("ens20", old.read_text())

    def test_failure_restores_overwritten_files_and_removes_new_files(self):
        old = self.write("/etc/network/interfaces", "iface lo inet loopback\n", 0o640)
        installer = self.make("router")
        installer.plan()
        original = files.atomic_write
        calls = 0

        def fail_once(*args):
            nonlocal calls
            calls += 1
            if calls == 3:
                raise OSError("fixture write failure")
            return original(*args)

        with (
            patch.object(files, "atomic_write", side_effect=fail_once),
            self.assertRaisesRegex(OSError, "fixture"),
        ):
            installer.install()
        self.assertEqual(old.read_text(), "iface lo inet loopback\n")
        self.assertEqual(stat.S_IMODE(old.stat().st_mode), 0o640)
        self.assertFalse((self.root / "etc/sysctl.d/90-forcad-router.conf").exists())
        self.assertFalse(installer.manifest.exists())

    def test_refuses_symlink_and_extra_interface_before_writing(self):
        target = self.write(
            "/etc/network/interfaces", "iface management0 inet static\n"
        )
        with self.assertRaisesRegex(ValueError, "extra devices"):
            self.make("router").plan()
        self.assertIn("management0", target.read_text())
        target.unlink()
        target.symlink_to(self.bundle / "router/interfaces")
        with self.assertRaisesRegex(ValueError, "symlink"):
            self.make("router").plan()

    def test_preserves_empty_includes_and_rejects_active_snippets(self):
        original = "source /etc/network/interfaces.d/*\niface lo inet loopback\n"
        target = self.write("/etc/network/interfaces", original)
        installer = self.make("router")
        installer.plan()
        self.assertTrue(
            installer.changes[target][0].startswith(
                b"source /etc/network/interfaces.d/*"
            )
        )
        self.write(
            "/etc/network/interfaces.d/management", "iface management0 inet static\n"
        )
        with self.assertRaisesRegex(ValueError, "manual merge"):
            self.make("router").plan()

    def test_reinstall_preserves_game_mode(self):
        active = self.write(
            "/etc/forcad/router/active.nft", 'include "/etc/forcad/router/game.nft"\n'
        )
        installer = self.make("router")
        installer.plan()
        self.assertEqual(installer.changes[active][0], active.read_bytes())

    def vpn_configs(self):
        installer = self.make("vpn")
        directory = self.bundle / "vpn/server"
        directory.mkdir()
        for name, expected in installer.config["wireguard"].items():
            private = base64.b64encode(bytes(range(32))).decode()
            content = f"[Interface]\nPrivateKey = {private}\nAddress = {expected['address']}\nListenPort = {expected['port']}\n"
            for number, peer in enumerate(expected["peers"]):
                public = base64.b64encode(bytes([number + 1]) * 32).decode()
                content += f"\n[Peer]\nPublicKey = {public}\nAllowedIPs = {peer}\n"
            (directory / f"{name}.conf").write_text(content)
        return installer, directory

    def test_vpn_requires_server_files_and_preserves_private_keys(self):
        with self.assertRaisesRegex(ValueError, "server"):
            self.make("vpn").plan()
        installer, directory = self.vpn_configs()
        installer.plan()
        server = (directory / "wg-attack.conf").read_text()
        self.write(
            "/etc/wireguard/wg-attack.conf",
            server.replace(base64.b64encode(bytes(range(32))).decode(), "other-key"),
        )
        with self.assertRaisesRegex(ValueError, "private key"):
            self.make("vpn").plan()

    def test_vpn_rejects_old_addresses_peers_and_firewall_hooks(self):
        installer, directory = self.vpn_configs()
        path = directory / "wg-attack.conf"
        good = path.read_text()
        for bad in (
            good.replace("10.183.200.1/24", "10.200.0.1/24"),
            good.replace("10.183.200.2/32", "10.200.0.2/32"),
            good.replace("[Interface]", "[Interface]\nPostUp = iptables -F"),
        ):
            with self.subTest(content=bad):
                path.write_text(bad)
                with self.assertRaises(ValueError):
                    installer.plan()

    def test_merges_identity_and_targets_without_replacing_other_settings(self):
        expected = render.teams(self.config)
        document = {
            "teams": [
                {
                    "name": "existing",
                    "token": "keep",
                    "ip": team["vulnbox"],
                    "traffic_cidrs": [team["vulnbox"] + "/32"],
                }
                for team in expected
            ],
            "game": {"round_time": 42},
        }
        merged = common.merge_teams(copy.deepcopy(document), expected)
        self.assertEqual(merged["game"], document["game"])
        self.assertTrue(all(team["token"] == "keep" for team in merged["teams"]))
        self.assertIn("10.183.200.0/29", merged["teams"][0]["traffic_cidrs"])
        document["teams"][0]["ip"] = "192.168.1.2"
        with self.assertRaisesRegex(ValueError, "match"):
            common.merge_teams(document, expected)
        groups = [{"targets": ["other:9109"], "labels": {"site": "keep"}}]
        expected_groups = copy.deepcopy(groups) + [{"targets": ["10.183.2.1:9109"]}]
        self.assertEqual(
            common.merge_targets(groups, "10.183.2.1:9109"), expected_groups
        )
        self.assertEqual(
            common.merge_targets(groups, "10.183.2.1:9109"), expected_groups
        )
        self.assertEqual(
            common.merge_env(
                "BRAND=keep\nPUBLIC_SCHEME=https\n", {"PUBLIC_SCHEME": "http"}
            ),
            b"BRAND=keep\nPUBLIC_SCHEME=http\n",
        )
        with self.assertRaisesRegex(ValueError, "Duplicate"):
            common.merge_env(
                "PUBLIC_SCHEME=http\nPUBLIC_SCHEME=https\n", {"PUBLIC_SCHEME": "http"}
            )

    @unittest.skipUnless(importlib.util.find_spec("yaml"), "python3-yaml required")
    def test_forcad_stages_discovery_without_touching_live_targets(self):
        repo = "/srv/ForcAD"
        self.write(repo + "/control.sh", "#!/bin/sh\n")
        document = {
            "teams": [
                {"ip": team["vulnbox"], "name": "original", "token": "keep"}
                for team in render.teams(self.config)
            ]
        }
        self.write(repo + "/config/config.yml", json.dumps(document))
        env = self.write(
            repo + "/deployment/docker/.env", "GRAFANA_ADMIN_PASSWORD=keep\n"
        )
        targets = self.write(
            repo + "/deployment/monitoring/file_sd/routers.yml",
            '[{"targets":["other:9109"]}]',
        )
        installer = self.make("forcad")
        installer.plan(repo=repo, network_metrics=True)
        installer.install()
        self.assertEqual(targets.read_text(), '[{"targets":["other:9109"]}]')
        self.assertIn("GRAFANA_ADMIN_PASSWORD=keep", env.read_text())
        self.assertIn(
            "10.183.2.1:9109",
            (self.root / "srv/ForcAD/config/runtime/network/routers.yml").read_text(),
        )
        self.assertTrue(
            all(
                team["token"] == "keep"
                for team in common.read_yaml(
                    self.root / "srv/ForcAD/config/config.yml"
                )["teams"]
            )
        )
        installer.uninstall()
        self.assertEqual(
            (self.root / "srv/ForcAD/config/config.yml").read_text(),
            json.dumps(document),
        )
        self.assertEqual(env.read_text(), "GRAFANA_ADMIN_PASSWORD=keep\n")
        self.assertEqual(targets.read_text(), '[{"targets":["other:9109"]}]')
        self.assertFalse(
            (self.root / "srv/ForcAD/config/runtime/network/routers.yml").exists()
        )

    def test_script_wrappers_are_portable_and_need_no_project_imports(self):
        for role in ("router", "vpn", "forcad"):
            script_dir = self.bundle / role / ("host" if role == "forcad" else "")
            for action in ("install", "test", "uninstall"):
                output = common.run("bash", str(script_dir / f"{action}.sh"), "--help")
                self.assertIn("--live", output)
        self.assertFalse(any(self.root.iterdir()))

    def test_metrics_reject_failed_scrapes_and_stale_heartbeats(self):
        response = MagicMock()
        response.status = 200
        response.__enter__.return_value = response
        opener = MagicMock()
        opener.open.return_value = response
        with patch.object(checks.urllib.request, "build_opener", return_value=opener):
            response.read.return_value = b"forcad_network_scrape_success 0\nforcad_network_source_scrape_success 1\n"
            with self.assertRaisesRegex(ValueError, "failed collection"):
                checks.http("http://10.183.2.1:9109/metrics", "network-metrics")
            response.read.return_value = b"forcad_network_scrape_success 1\nforcad_network_source_scrape_success 1\n"
            checks.http("http://10.183.2.1:9109/metrics", "network-metrics")
            response.read.return_value = b"forcad_game_sync_timestamp_seconds 0\n"
            with self.assertRaisesRegex(ValueError, "stale"):
                checks.http("http://10.183.2.1:9911/metrics", "game-metrics")


if __name__ == "__main__":
    unittest.main()

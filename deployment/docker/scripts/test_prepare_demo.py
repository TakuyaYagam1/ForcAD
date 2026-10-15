"""Check demo address migration without starting containers or changing a database."""

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
    def test_old_inventories_migrate_and_keep_tokens_and_keys(self):
        for addresses in (*prepare_demo.OLD_IPS, ["10.183.101.2", "10.183.102.2"]):
            with (
                self.subTest(addresses=addresses),
                tempfile.TemporaryDirectory() as directory,
            ):
                root = Path(directory)
                for relative in (
                    "deployment/demo/network.toml",
                    "deployment/monitoring/prometheus.yml",
                ):
                    destination = root / relative
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(ROOT / relative, destination)
                config = root / "config/config.yml"
                config.parent.mkdir()
                teams = [
                    {
                        "name": f"Team {index + 1}",
                        "ip": address,
                        "token": f"{index + 1:016x}",
                    }
                    for index, address in enumerate(addresses)
                ]
                config.write_text(yaml.safe_dump({"teams": teams}))
                original = config.read_bytes()
                output = root / "config/runtime/demo"
                output.parent.mkdir()
                network = prepare_demo.render.load_config(
                    root / "deployment/demo/network.toml"
                )
                prepare_demo.render.write_bundle(
                    output, prepare_demo.render.render(network)
                )
                wireguard = output / "wireguard"
                wireguard.mkdir(parents=True)
                shutil.copyfile(
                    root / "deployment/demo/network.toml", output / "network.toml"
                )
                (wireguard / "server").mkdir()
                server = wireguard / "server/wg-attack.conf"
                server.write_text(
                    "[Interface]\nPrivateKey = keep-private\nAddress = 10.200.0.1/24\n"
                    "[Peer]\nPublicKey = keep-public\nPresharedKey = keep-psk\nAllowedIPs = 10.200.0.2/32\n"
                )
                (wireguard / "participants/team-01").mkdir(parents=True)
                client = wireguard / "participants/team-01/member-01.conf"
                client.write_text(
                    "[Interface]\nPrivateKey = keep-client\nAddress = 10.200.0.2/32\n"
                    "[Peer]\nPublicKey = keep-server\nAllowedIPs = 10.0.0.0/8\n"
                )
                (wireguard / "assignments.csv").write_text(
                    "1,1,10.200.0.0/29,10.200.0.2\n"
                )
                before = {
                    p.relative_to(wireguard): p.read_bytes()
                    for p in wireguard.rglob("*")
                    if p.is_file()
                }

                with (
                    patch.object(sys, "argv", ["prepare_demo.py", str(root)]),
                    contextlib.redirect_stdout(io.StringIO()),
                ):
                    prepare_demo.main()
                    first_config = config.read_bytes()
                    prepare_demo.main()

                updated = yaml.safe_load(config.read_text())["teams"]
                self.assertEqual(
                    [t["ip"] for t in updated], ["10.183.101.2", "10.183.102.2"]
                )
                self.assertEqual(
                    [t["token"] for t in updated], [t["token"] for t in teams]
                )
                self.assertEqual(
                    updated[1]["traffic_cidrs"], ["10.183.102.2/32", "10.183.200.8/29"]
                )
                self.assertEqual(first_config, config.read_bytes())
                self.assertEqual(
                    (root / "config/config_backup_demo_network.yml").read_bytes(),
                    original,
                )
                for relative, content in before.items():
                    self.assertEqual(
                        (output / "wireguard-before-network" / relative).read_bytes(),
                        content,
                    )
                self.assertIn("PrivateKey = keep-private", server.read_text())
                self.assertIn("PresharedKey = keep-psk", server.read_text())
                self.assertIn("AllowedIPs = 10.183.200.2/32", server.read_text())
                self.assertIn("Address = 10.183.200.2/32", client.read_text())
                self.assertIn("AllowedIPs = 10.183.0.0/16", client.read_text())
                self.assertIn("PrivateKey = keep-client", client.read_text())


if __name__ == "__main__":
    unittest.main()

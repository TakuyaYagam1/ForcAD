import importlib.util
import ipaddress
import shutil
import stat
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
import render

spec = importlib.util.spec_from_file_location("generate", ROOT / "vpn/generate.py")
generate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generate)


class RenderTests(unittest.TestCase):
    def setUp(self):
        self.config = render.load_config(ROOT / "network.example.toml")

    def test_allocations_are_unique_and_do_not_use_server_addresses(self):
        self.config["teams"] = 32
        allocated = render.teams(self.config)
        peers = [ip for team in allocated for ip in team["peers"]]
        self.assertEqual(len(peers), 160)
        self.assertEqual(len(set(peers)), 160)
        self.assertNotIn("10.183.200.1", peers)
        for team in allocated:
            subnet = ipaddress.ip_network(team["vpn_cidr"])
            self.assertTrue(
                all(ipaddress.ip_address(ip) in subnet for ip in team["peers"])
            )

    def test_rejects_invalid_and_injectable_config(self):
        example = (ROOT / "network.example.toml").read_text()
        bad_inputs = [
            example.replace('wan_interface = "eth0"', 'wan_interface = "eth0; reboot"'),
            example.replace('team_trunk = "eth3"', 'team_trunk = "eth0"'),
            example.replace("teams = 20", "teams = 33"),
            example.replace("teams = 20", "teams = true"),
            example.replace("members_per_team = 5", "members_per_team = 6"),
            example.replace("game_tcp_ports = [8080, 8081]", "game_tcp_ports = [22]"),
            example.replace("game_udp_ports = []", "game_udp_ports = [0]"),
            example.replace("game_udp_ports = []", "game_udp_ports = [53, 53]"),
            example.replace("admin_port = 56768", "admin_port = 56767"),
            example.replace('203.0.113.1"', '203.0.114.1"'),
            example.replace("203.0.113.", "10.183.1."),
            example.replace("203.0.113.", "10.183.200."),
            example.replace("vpn.example.org", "vpn.example.org:51820"),
            example.replace("teams = 20", "teams = 20\nteamz = 20"),
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "network.toml"
            for text in bad_inputs:
                with self.subTest(config=text):
                    path.write_text(text)
                    with self.assertRaises(ValueError):
                        render.load_config(path)

    def test_team_networks_are_disjoint_inside_the_game_prefix(self):
        self.config["teams"] = 32
        allocated = render.teams(self.config)
        networks = [ipaddress.ip_network(team["network"]) for team in allocated]
        self.assertEqual(str(networks[0]), "10.183.101.0/24")
        self.assertEqual(str(networks[-1]), "10.183.132.0/24")
        networks += [
            render.ATTACK_NET,
            render.ADMIN_NET,
            ipaddress.ip_network("10.183.1.0/30"),
            ipaddress.ip_network("10.183.2.0/24"),
        ]
        for index, network in enumerate(networks):
            self.assertTrue(network.subnet_of(render.INTERNAL_NET))
            self.assertFalse(
                any(network.overlaps(other) for other in networks[index + 1 :])
            )
        for team in allocated:
            network = ipaddress.ip_network(team["network"])
            self.assertEqual(team["gateway"], str(network[1]))
            self.assertEqual(team["vulnbox"], str(network[2]))

    def test_wan_can_use_a_nonoverlapping_private_network(self):
        example = (ROOT / "network.example.toml").read_text()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "network.toml"
            path.write_text(example.replace("203.0.113.", "10.0.2."))
            self.assertEqual(
                render.load_config(path)["router"]["wan_address"], "10.0.2.10/24"
            )

    def test_output_is_private_and_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "bundle"
            render.write_bundle(target, {"nested/deeper/file": "original\n"})
            for path in [target, *target.rglob("*")]:
                self.assertEqual(
                    stat.S_IMODE(path.stat().st_mode), 0o700 if path.is_dir() else 0o600
                )
            with self.assertRaises(FileExistsError):
                render.write_bundle(target, {"nested/deeper/file": "replacement\n"})
            self.assertEqual((target / "nested/deeper/file").read_text(), "original\n")
            link = Path(directory) / "link"
            link.symlink_to(target, target_is_directory=True)
            with self.assertRaises(FileExistsError):
                render.write_bundle(link, {})

    def test_render_has_no_secrets_and_supports_udp_only_game(self):
        self.config["game_tcp_ports"] = []
        self.config["game_udp_ports"] = [31337]
        files = render.render(self.config)
        content = "\n".join(files.values())
        self.assertNotIn("@@", content)
        self.assertNotIn("PrivateKey", content)
        self.assertNotIn("PresharedKey", content)
        self.assertNotIn("flush ruleset", content)
        self.assertIn("meta l4proto udp ct original proto-dst { 31337 }", content)
        self.assertIn("10.183.200.0/29", files["forcad/teams.yml"])
        self.assertIn("10.183.101.2/32", files["forcad/teams.yml"])

    @unittest.skipUnless(shutil.which("wg"), "wireguard-tools is required")
    def test_new_wireguard_bundle_has_distinct_keys_and_exact_peer_addresses(self):
        self.config.update(teams=2, members_per_team=2, admins=2)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "wireguard"
            generate.generate(self.config, output)
            configs = list(output.rglob("*.conf"))
            self.assertEqual(len(configs), 8)
            private_keys = []
            for path in configs:
                content = path.read_text()
                private = next(
                    line.removeprefix("PrivateKey = ")
                    for line in content.splitlines()
                    if line.startswith("PrivateKey = ")
                )
                private_keys.append(private)
                public = generate.wg("pubkey", private + "\n")
                self.assertEqual(len(public), 44)
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
                if path.parent.name != "server":
                    self.assertIn("AllowedIPs = 10.183.0.0/16", content)
                    self.assertNotIn("AllowedIPs = 10.0.0.0/8", content)
                    matching_server = (
                        "wg-admin.conf"
                        if path.parent.name == "admins"
                        else "wg-attack.conf"
                    )
                    self.assertIn(
                        "PublicKey = " + public,
                        (output / "server" / matching_server).read_text(),
                    )
            self.assertTrue(
                len(set(private_keys)) == len(private_keys),
                "Private keys must be distinct",
            )
            server = (output / "server/wg-attack.conf").read_text()
            for address in [
                "10.183.200.2",
                "10.183.200.3",
                "10.183.200.9",
                "10.183.200.10",
            ]:
                self.assertIn(f"AllowedIPs = {address}/32\n", server)
            self.assertNotIn("PostUp", server)
            self.assertNotIn("PreUp", server)
            with self.assertRaises(FileExistsError):
                generate.generate(self.config, output)


if __name__ == "__main__":
    unittest.main()

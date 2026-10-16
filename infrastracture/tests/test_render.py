import hashlib
import importlib.util
import ipaddress
import json
import os
import shutil
import stat
import subprocess
import sys
import tarfile
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
        self.config = render.load_config(ROOT / "settings.example.toml")

    def test_allocations_are_unique_and_do_not_use_server_addresses(self):
        self.config["teams"]["entries"] = [
            {"name": f"team-{number:02}", "logo": None, "highlighted": False}
            for number in range(1, 33)
        ]
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
        example = (ROOT / "settings.example.toml").read_text()
        bad_inputs = [
            example.replace(
                'wan_interface = "ens19"', 'wan_interface = "ens19; reboot"'
            ),
            example.replace('team_trunk = "ens21"', 'team_trunk = "ens19"'),
            example.replace("members_per_team = 5", "members_per_team = 6"),
            example.replace("game_tcp_ports = [8080, 8081]", "game_tcp_ports = [22]"),
            example.replace("game_udp_ports = []", "game_udp_ports = [0]"),
            example.replace("game_udp_ports = []", "game_udp_ports = [53, 53]"),
            example.replace("admin_port = 56768", "admin_port = 56767"),
            example.replace('203.0.113.1"', '203.0.114.1"'),
            example.replace("203.0.113.", "10.183.1."),
            example.replace("203.0.113.", "10.183.200."),
            example.replace(
                'admin_vpn = "10.183.201.0/24"',
                'admin_vpn = "10.183.200.0/24"',
            ),
            example.replace(
                'participant_vpn = "10.183.200.0/24"',
                'participant_vpn = "10.183.200.0/29"',
            ),
            example.replace("team_vlan_start = 101", "team_vlan_start = 4094"),
            example.replace("vpn.example.org", "vpn.example.org:51820"),
            example.replace("admins = 1", "admins = 1\nteamz = 20"),
            example.replace(
                'public_host = "10.183.2.10"', 'public_host = "10.183.3.11"'
            ),
            example.replace('logo = "danosito.jpg"', 'logo = "../danosito.jpg"'),
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "settings.toml"
            for text in bad_inputs:
                with self.subTest(config=text):
                    path.write_text(text)
                    with self.assertRaises(ValueError):
                        render.load_config(path)

    def test_team_networks_do_not_overlap_other_configured_networks(self):
        self.config["teams"]["entries"] = [
            {"name": f"team-{number:02}", "logo": None, "highlighted": False}
            for number in range(1, 33)
        ]
        allocated = render.teams(self.config)
        networks = [ipaddress.ip_network(team["network"]) for team in allocated]
        self.assertEqual(str(networks[0]), "10.183.101.0/24")
        self.assertEqual(str(networks[-1]), "10.183.132.0/24")
        layout = render.network_layout(self.config)
        networks += [
            layout["router_vpn"],
            layout["forcad"],
            layout["participant_vpn"],
            layout["admin_vpn"],
        ]
        for index, network in enumerate(networks):
            self.assertFalse(
                any(network.overlaps(other) for other in networks[index + 1 :])
            )
        for team in allocated:
            network = ipaddress.ip_network(team["network"])
            self.assertEqual(team["gateway"], str(network[1]))
            self.assertEqual(team["vulnbox"], str(network[2]))

    def test_wan_can_use_a_nonoverlapping_private_network(self):
        example = (ROOT / "settings.example.toml").read_text()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "settings.toml"
            path.write_text(example.replace("203.0.113.", "10.0.2."))
            self.assertEqual(
                render.load_config(path)["router"]["wan_address"], "10.0.2.10/24"
            )

    def test_custom_network_settings_drive_generated_files(self):
        example = (ROOT / "settings.example.toml").read_text()
        custom = (
            example.replace("10.183.1.0/30", "192.168.10.0/30")
            .replace("10.183.2.0/24", "172.25.2.0/24")
            .replace('public_host = "10.183.2.10"', 'public_host = "172.25.2.20"')
            .replace("10.183.200.0/24", "172.26.200.0/24")
            .replace("10.183.201.0/24", "172.26.201.0/24")
            .replace("10.183.101.0/24", "172.27.101.0/24")
        )
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "settings.toml"
            path.write_text(custom)
            config = render.load_config(path)
            files = render.render(config)
        self.assertIn("address 192.168.10.1/30", files["router/interfaces"])
        self.assertIn("address 172.25.2.20/24", files["forcad/interfaces"])
        self.assertIn("address 172.27.101.1/24", files["router/interfaces"])
        self.assertIn("172.26.200.0/24", files["router/interfaces"])
        self.assertIn("172.27.101.2", files["forcad/config.yml"])
        self.assertIn("172.25.2.1:9109", files["forcad/routers.yml"])
        self.assertIn("define attack_net = 172.26.200.0/24", files["vpn/firewall.nft"])
        self.assertIn("define router_ip = 192.168.10.1", files["vpn/firewall.nft"])
        self.assertIn("172.25.2.1:9911", files["forcad/game-sensors.yml"])
        self.assertIn("172.25.2.1:9109", files["router/network-metrics.service"])
        host = json.loads(files["forcad/host.json"])
        self.assertEqual(
            host["checks"]["forcad_url"],
            "http://172.25.2.20/api/client/status/",
        )
        self.assertNotIn("10.183.", "\n".join(files["router/interfaces"].splitlines()))

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

    def test_archives_are_private_and_vpn_archive_includes_generated_server_keys(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "runtime"
            output.mkdir(mode=0o700)
            for role in ("vpn", "router", "forcad"):
                render.write_bundle(output / role, {"install.sh": "#!/bin/sh\n"})
            wireguard_server = output / "wireguard/server"
            wireguard_server.mkdir(mode=0o700, parents=True)
            (wireguard_server / "wg-attack.conf").write_text("PrivateKey = test\n")

            render.create_archives(output, wireguard_server=wireguard_server)

            for role in ("vpn", "router", "forcad"):
                archive = output / f"{role}.tar.gz"
                self.assertEqual(stat.S_IMODE(archive.stat().st_mode), 0o600)
                with tarfile.open(archive, "r:gz") as bundle:
                    names = bundle.getnames()
                    self.assertIn("install.sh", names)
                    if role == "vpn":
                        self.assertIn("server/wg-attack.conf", names)
                    else:
                        self.assertEqual(names, ["install.sh"])

    def test_render_cli_writes_three_archives(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "runtime"
            existing = Path(directory) / "existing"
            existing.mkdir()
            marker = existing / "keep"
            marker.write_text("preserve\n")
            refused = subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "render.py"),
                    "--config",
                    str(ROOT / "settings.example.toml"),
                    "--output",
                    str(existing),
                ],
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(refused.returncode, 0)
            self.assertEqual(marker.read_text(), "preserve\n")

            subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "render.py"),
                    "--config",
                    str(ROOT / "settings.example.toml"),
                    "--output",
                    str(output),
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertEqual(
                {path.name for path in output.glob("*.tar.gz")},
                {"vpn.tar.gz", "router.tar.gz", "forcad.tar.gz"},
            )

    def test_render_has_no_secrets_and_supports_udp_only_game(self):
        self.config["game_tcp_ports"] = []
        self.config["game_udp_ports"] = [31337]
        files = render.render(self.config)
        content = "\n".join(value for value in files.values() if isinstance(value, str))
        self.assertNotIn("@@", content)
        self.assertNotIn("PrivateKey = ", content)
        self.assertNotIn("PresharedKey = ", content)
        self.assertNotIn("flush ruleset", content)
        self.assertIn("meta l4proto udp ct original proto-dst { 31337 }", content)
        self.assertIn("10.183.200.0/29", files["forcad/config.yml"])
        self.assertIn("10.183.101.2/32", files["forcad/config.yml"])
        self.assertIn(
            'logo_path: "/team-logos/danosito.jpg"', files["forcad/config.yml"]
        )
        self.assertIn("forcad/checkers/examples/note/checker.py", files)
        self.assertIn("forcad/public/teams/danosito.jpg", files)
        self.assertIn("Fresh installation", files["forcad/install.sh"])
        self.assertLess(
            files["forcad/install.sh"].index("apt-get update"),
            files["forcad/install.sh"].index("git clone"),
        )
        self.assertIn("docker volume ls -q", files["forcad/install.sh"])
        self.assertIn(".forcad-managed-packages", files["forcad/install.sh"])
        self.assertIn("down -v --remove-orphans", files["forcad/uninstall.sh"])
        self.assertIn('rm -rf -- "$target"', files["forcad/uninstall.sh"])
        self.assertIn("apt-get purge -y", files["forcad/uninstall.sh"])
        self.assertIn(
            "/var/backups/forcad/forcad/install-", files["forcad/uninstall.sh"]
        )
        self.assertNotIn("update.sh", files)
        self.assertIn(
            "down ip route del 10.183.200.0/24 via 10.183.1.2 dev ens20 || true",
            files["router/interfaces"],
        )
        self.assertIn(
            "down ip route del 10.183.201.0/24 via 10.183.1.2 dev ens20 || true",
            files["router/interfaces"],
        )
        bundle = render.host_bundle(self.config)
        self.assertIn("vpn/vpnlib/lifecycle.py", bundle)

    def test_team_without_logo_is_rendered_without_logo_path(self):
        self.config["teams"]["entries"][0]["logo"] = ""
        rendered = render.forcad_config(self.config)
        team_section = rendered.split("teams:\n", 1)[1]
        first_team = team_section.split("  - name:", 2)[1]
        self.assertNotIn("logo_path:", first_team)

    def test_forcad_uninstaller_requires_confirmation_and_removes_only_owned_packages(
        self,
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "forcad"
            target.mkdir()
            package_file = target / ".forcad-managed-packages"
            package_file.write_text("forcad-example\n")
            package_hash = hashlib.sha256(package_file.read_bytes()).hexdigest()
            marker = target / ".forcad-fresh-install"
            marker.write_text(
                "https://github.com/TakuyaYagam1/ForcAD.git main\n"
                f"packages_sha256={package_hash}\n"
            )
            (target / ".forcad-host-backup").write_text("20261016-130000-example\n")
            (target / ".forcad-docker-state").write_text("active\nenabled\n")
            control = target / "control.sh"
            control.write_text(
                '#!/usr/bin/env bash\nprintf "control %s\\n" "$*" >> "$CALL_LOG"\n'
            )
            control.chmod(0o755)

            bundle = root / "bundle"
            host = bundle / "host"
            host.mkdir(parents=True)
            host_uninstall = host / "uninstall.sh"
            host_uninstall.write_text("#!/usr/bin/env bash\nexit 0\n")
            host_uninstall.chmod(0o755)

            config = dict(self.config)
            config["forcad"] = dict(self.config["forcad"], install_path=str(target))
            uninstaller = render.fresh_forcad_bundle(config)["forcad/uninstall.sh"]
            uninstaller = uninstaller.replace(
                'if [[ "$EUID" -ne 0 ]]; then exec sudo -- bash "$0" "$@"; fi\n',
                "",
            )
            script = bundle / "uninstall.sh"
            script.write_text(uninstaller)

            fake_bin = root / "bin"
            fake_bin.mkdir()
            call_log = root / "calls.log"
            for command in ("apt-get", "systemctl", "rmdir"):
                executable = fake_bin / command
                executable.write_text(
                    "#!/usr/bin/env bash\n"
                    f'printf "{command} %s\\n" "$*" >> "$CALL_LOG"\n'
                )
                executable.chmod(0o755)
            environment = dict(os.environ, PATH=f"{fake_bin}:{os.environ['PATH']}")
            environment["CALL_LOG"] = str(call_log)

            refused = subprocess.run(
                ["bash", str(script)],
                env=environment,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(refused.returncode, 2)
            self.assertTrue(target.is_dir())
            self.assertFalse(call_log.exists())

            completed = subprocess.run(
                ["bash", str(script), "--confirm"],
                env=environment,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertFalse(target.exists())
            calls = call_log.read_text()
            self.assertIn("apt-get purge -y -- forcad-example", calls)
            self.assertIn("systemctl enable docker", calls)
            self.assertIn("systemctl start docker", calls)
            self.assertNotIn("autoremove", calls)

    @unittest.skipUnless(shutil.which("wg"), "wireguard-tools is required")
    def test_new_wireguard_bundle_has_distinct_keys_and_exact_peer_addresses(self):
        self.config["teams"]["entries"] = self.config["teams"]["entries"][:2]
        self.config["teams"]["members_per_team"] = 2
        self.config["admins"] = 2
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
                    self.assertIn("AllowedIPs = 10.183.1.0/30", content)
                    self.assertIn("10.183.101.0/24", content)
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

            self.config["network"].update(
                router_vpn="192.168.10.0/30",
                forcad="172.25.2.0/24",
                participant_vpn="172.26.200.0/24",
                admin_vpn="172.27.201.0/24",
                team_network_base="172.27.101.0/24",
            )
            self.config["env"]["public_host"] = "172.25.2.20"
            custom_root = Path(directory) / "custom"
            custom_root.mkdir()
            custom_output = custom_root / "wireguard"
            generate.generate(self.config, custom_output)
            participant = (custom_output / "server/wg-attack.conf").read_text()
            admin = (custom_output / "server/wg-admin.conf").read_text()
            client_path = next((custom_output / "participants").rglob("member-01.conf"))
            client = client_path.read_text()
            self.assertIn("Address = 172.26.200.1/24", participant)
            self.assertIn("Address = 172.27.201.1/24", admin)
            self.assertIn("AllowedIPs = 192.168.10.0/30", client)
            self.assertIn("172.25.2.0/24", client)
            self.assertIn("172.27.101.0/24", client)


if __name__ == "__main__":
    unittest.main()

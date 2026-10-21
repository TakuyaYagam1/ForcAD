import base64
import hashlib
import importlib.util
import io
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
from pathlib import Path, PurePosixPath
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(ROOT / "tests"))
from helpers import common, source_bundle

import render

spec = importlib.util.spec_from_file_location("generate", ROOT / "vpn/generate.py")
generate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generate)


def extract_archive(bundle, destination):
    """Extract regular files while keeping the test portable before Python 3.12."""
    destination = Path(destination)
    for member in bundle.getmembers():
        name = PurePosixPath(member.name)
        if name.is_absolute() or ".." in name.parts or not member.isfile():
            raise AssertionError(f"Unsafe archive member: {member.name}")
        target = destination.joinpath(*name.parts)
        target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        stream = bundle.extractfile(member)
        if stream is None:
            raise AssertionError(f"Unreadable archive member: {member.name}")
        descriptor = os.open(
            target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, member.mode & 0o755
        )
        with stream, os.fdopen(descriptor, "wb") as output:
            shutil.copyfileobj(stream, output)
            os.fchmod(output.fileno(), member.mode & 0o755)


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
            example.replace("extra_checks = 0", "extra_checks = -1"),
            example.replace("extra_checks = 0", "extra_checks = 9"),
            example.replace("extra_checks = 0", "extra_checks = true"),
            example.replace("extra_checks = 0", "extra_checks = 1.5"),
            example.replace("extra_checks = 0", "extra_checks = 2").replace(
                'mode = "classic"', 'mode = "blitz"'
            ),
            example.replace("extra_checks = 0", "extra_checks = 2").replace(
                "round_time = 60", "round_time = 13"
            ),
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

    def test_extra_checks_are_optional_and_rendered(self):
        example = (ROOT / "settings.example.toml").read_text()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "settings.toml"
            for source in (example, example.replace("extra_checks = 0\n", "")):
                with self.subTest(explicit_setting="extra_checks" in source):
                    path.write_text(source)
                    config = render.load_config(path)
                    self.assertEqual(config["game"]["extra_checks"], 0)
                    self.assertNotIn("extra_checks:", render.forcad_config(config))
            path.write_text(example.replace("extra_checks = 0", "extra_checks = 2"))
            config = render.load_config(path)
            self.assertIn("  extra_checks: 2\n", render.forcad_config(config))

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
        inventory = json.loads(files["router/network.json"])
        self.assertEqual(
            inventory["teams"][0],
            {
                "team_ip": "172.27.101.2",
                "networks": ["172.27.101.2/32", "172.26.200.0/29"],
            },
        )
        self.assertNotIn("10.183.", files["router/network.json"])

    def test_network_inventory_contains_only_team_addresses_and_traffic_networks(self):
        for count in (1, 5, 32):
            with self.subTest(teams=count):
                self.config["teams"]["entries"] = [
                    {"name": f"team-{number}", "logo": "", "highlighted": False}
                    for number in range(count)
                ]
                files = render.render(self.config)
                expected = {
                    "teams": [
                        {
                            "team_ip": f"10.183.{101 + number}.2",
                            "networks": [
                                f"10.183.{101 + number}.2/32",
                                f"10.183.200.{number * 8}/29",
                            ],
                        }
                        for number in range(count)
                    ]
                }
                self.assertEqual(json.loads(files["router/network.json"]), expected)

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

    def test_archives_are_role_prefixed_private_and_extractable(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "runtime"
            output.mkdir(mode=0o700)
            files_by_role = {
                "vpn": {
                    "host.py": "vpn host\n",
                    "firewall.nft": "vpn firewall\n",
                    "server/wg-admin.conf": "PrivateKey = test\n",
                },
                "router": {
                    "host.py": "router host\n",
                    "network.json": "{}\n",
                    "rules/firewall.nft": "router firewall\n",
                },
                "forcad": {
                    "host.py": "forcad host\n",
                    "config.yml": "teams: []\n",
                    "checkers/check.py": "print('ok')\n",
                },
            }
            expected = {
                role: {f"{role}/{name}": content for name, content in files.items()}
                for role, files in files_by_role.items()
            }
            expected["vpn"]["vpn/server/wg-admin.conf"] = "PrivateKey = test\n"
            for role, files in files_by_role.items():
                render.write_bundle(output / role, files)
            wireguard_server = output / "wireguard/server"
            wireguard_server.mkdir(mode=0o700, parents=True)
            (wireguard_server / "wg-admin.conf").write_text("PrivateKey = test\n")

            render.create_archives(output, wireguard_server=wireguard_server)

            extract_to = Path(directory) / "extracted"
            extract_to.mkdir()
            for role in ("vpn", "router", "forcad"):
                archive = output / f"{role}.tar.gz"
                self.assertEqual(stat.S_IMODE(archive.stat().st_mode), 0o600)
                with tarfile.open(archive, "r:gz") as bundle:
                    names = bundle.getnames()
                    self.assertCountEqual(names, expected[role])
                    self.assertTrue(all(name.startswith(f"{role}/") for name in names))
                    extract_archive(bundle, extract_to)
            for archive_files in expected.values():
                for name, content in archive_files.items():
                    extracted = extract_to / name
                    self.assertTrue(extracted.is_file())
                    self.assertEqual(extracted.read_text(), content)

    def test_installed_public_assets_are_readable_and_config_stays_private(self):
        files = render.fresh_forcad_files(self.config)
        script = files["forcad/install.sh"]
        copy_assets = script[
            script.index('install -m 0600 "$bundle_dir/config.yml"') : script.index(
                'if [[ -d "$bundle_dir/checkers" ]]'
            )
        ]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / "runtime"
            render.write_bundle(output, files)
            render.create_archives(output, roles=("forcad",))
            extracted = root / "extracted"
            with tarfile.open(output / "forcad.tar.gz") as archive:
                extract_archive(archive, extracted)
            target = root / "installed"
            for name in ("config", "deployment/docker", "public/teams"):
                (target / name).mkdir(parents=True, exist_ok=True)
            subprocess.run(
                [
                    "bash",
                    "-c",
                    'set -euo pipefail\numask 077\nbundle_dir="$1"\ntarget="$2"\n'
                    + copy_assets,
                    "install-assets",
                    str(extracted / "forcad"),
                    str(target),
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            public = target / "public"
            for path in (public, *public.rglob("*")):
                self.assertEqual(
                    stat.S_IMODE(path.stat().st_mode),
                    0o755 if path.is_dir() else 0o644,
                    str(path.relative_to(target)),
                )
                if path.is_file():
                    self.assertEqual(
                        path.read_bytes(),
                        files[f"forcad/{path.relative_to(target).as_posix()}"],
                    )
            for name in ("config/config.yml", "deployment/docker/.env"):
                self.assertEqual(stat.S_IMODE((target / name).stat().st_mode), 0o600)

    def test_archive_collision_preserves_existing_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "runtime"
            output.mkdir(mode=0o700)
            render.write_bundle(
                output / "vpn", {"server/wg-admin.conf": "PrivateKey = bundled\n"}
            )
            wireguard_server = output / "wireguard/server"
            wireguard_server.mkdir(mode=0o700, parents=True)
            (wireguard_server / "wg-admin.conf").write_text("PrivateKey = external\n")
            archive = output / "vpn.tar.gz"
            archive.write_bytes(b"existing archive\n")

            with self.assertRaisesRegex(ValueError, "differs from bundle"):
                render.create_archives(
                    output, roles=("vpn",), wireguard_server=wireguard_server
                )

            self.assertEqual(archive.read_bytes(), b"existing archive\n")
            self.assertEqual(list(output.glob(".vpn.*.tmp")), [])

    def test_archive_collision_cleans_standalone_wireguard_output(self):
        with tempfile.TemporaryDirectory() as directory:
            runtime = Path(directory) / "runtime"
            runtime.mkdir(mode=0o700)
            render.write_bundle(
                runtime / "vpn", {"server/wg-attack.conf": "old server config\n"}
            )
            archive = runtime / "vpn.tar.gz"
            archive.write_bytes(b"existing archive\n")
            output = runtime / "wireguard"
            counter = 0

            def fake_wg(command, value=None):
                nonlocal counter
                counter += 1
                return base64.b64encode(counter.to_bytes(32, "big")).decode()

            with (
                mock.patch.object(generate, "wg", side_effect=fake_wg),
                self.assertRaisesRegex(ValueError, "differs from bundle"),
            ):
                generate.generate(self.config, output)

            self.assertFalse(output.exists())
            self.assertEqual(archive.read_bytes(), b"existing archive\n")

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

            fake_bin = Path(directory) / "bin"
            fake_bin.mkdir()
            host_arch = "amd64"
            router_arch = "arm64"
            fake_go = fake_bin / "go"
            fake_go.write_text(
                f"#!{sys.executable}\n"
                "import json\n"
                "from pathlib import Path\n"
                "import sys\n"
                f"host_arch = {host_arch!r}\n"
                "args = sys.argv[1:]\n"
                "if args[:1] == ['env']:\n"
                "    print(json.dumps({'GOHOSTOS': 'linux', 'GOHOSTARCH': host_arch}))\n"
                "elif args[:1] == ['build']:\n"
                "    output = Path(args[args.index('-o') + 1])\n"
                "    output.write_bytes(bytes.fromhex('7f454c46') + b' fixture\\n')\n"
                "elif args[:1] == ['run']:\n"
                "    print('create table inet forcad_monitoring')\n"
                "else:\n"
                "    raise SystemExit(f'Unexpected fake Go command: {args!r}')\n"
            )
            fake_go.chmod(0o700)
            fake_wg = fake_bin / "wg"
            fake_wg.write_text(
                f"#!{sys.executable}\n"
                "import base64\n"
                "import sys\n"
                "from pathlib import Path\n"
                "counter = Path(__file__).with_suffix('.count')\n"
                "try:\n"
                "    number = int(counter.read_text()) + 1\n"
                "except FileNotFoundError:\n"
                "    number = 1\n"
                "counter.write_text(str(number))\n"
                "if sys.argv[1] not in {'genkey', 'genpsk', 'pubkey'}:\n"
                "    raise SystemExit(2)\n"
                "print(base64.b64encode(number.to_bytes(32, 'big')).decode())\n"
            )
            fake_wg.chmod(0o700)
            environment = dict(os.environ, PATH=f"{fake_bin}:{os.environ['PATH']}")

            subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "render.py"),
                    "--config",
                    str(ROOT / "settings.example.toml"),
                    "--output",
                    str(output),
                    "--router-arch",
                    router_arch,
                ],
                env=environment,
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertEqual(
                {path.name for path in output.glob("*.tar.gz")},
                {"vpn.tar.gz", "router.tar.gz", "forcad.tar.gz"},
            )

            wireguard = output / "wireguard"
            server_files = ("wg-attack.conf", "wg-admin.conf")
            server_contents = {}
            for name in server_files:
                generated = (wireguard / "server" / name).read_bytes()
                bundled = (output / "vpn/server" / name).read_bytes()
                self.assertIn(b"PrivateKey = ", generated)
                self.assertEqual(bundled, generated)
                self.assertEqual(
                    stat.S_IMODE((output / "vpn/server" / name).stat().st_mode),
                    0o600,
                )
                server_contents[name] = generated

            participants = list((wireguard / "participants").rglob("*.conf"))
            admins = list((wireguard / "admins").glob("*.conf"))
            config = render.load_config(ROOT / "settings.example.toml")
            self.assertEqual(
                len(participants),
                len(config["teams"]["entries"]) * config["teams"]["members_per_team"],
            )
            self.assertEqual(len(admins), config["admins"])
            with tarfile.open(output / "vpn.tar.gz", "r:gz") as bundle_archive:
                names = bundle_archive.getnames()
                for name, content in server_contents.items():
                    archived = f"vpn/server/{name}"
                    self.assertEqual(names.count(archived), 1)
                    self.assertEqual(
                        bundle_archive.extractfile(archived).read(), content
                    )
                self.assertFalse(
                    any(
                        name.startswith(("vpn/participants/", "vpn/admins/"))
                        for name in names
                    )
                )

            before_repeat = {
                name: (output / "vpn/server" / name).read_bytes()
                for name in server_files
            }
            repeated = subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "render.py"),
                    "--config",
                    str(ROOT / "settings.example.toml"),
                    "--output",
                    str(output),
                    "--router-arch",
                    router_arch,
                ],
                env=environment,
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(repeated.returncode, 0)
            self.assertEqual(
                before_repeat,
                {
                    name: (output / "vpn/server" / name).read_bytes()
                    for name in server_files
                },
            )

            extracted = Path(directory) / "extracted"
            extracted.mkdir()
            with tarfile.open(output / "vpn.tar.gz", "r:gz") as bundle_archive:
                extract_archive(bundle_archive, extracted)
            host_root = Path(directory) / "host"
            host_root.mkdir()
            wireguard_dir = host_root / "etc/wireguard"
            wireguard_dir.mkdir(mode=0o700, parents=True)
            unrelated = wireguard_dir / "wg-unrelated.conf"
            unrelated.write_text("unrelated config\n")
            unrelated.chmod(0o600)
            vpn = source_bundle("vpn", extracted / "vpn", host_root)
            self.assertTrue(vpn.files_only)

            with (
                mock.patch.object(
                    common, "run", side_effect=AssertionError("runtime command")
                ),
                mock.patch(
                    "vpn.host.run", side_effect=AssertionError("runtime command")
                ),
                mock.patch(
                    "hostlib.base.run", side_effect=AssertionError("runtime command")
                ),
            ):
                vpn.plan()
                vpn.install()
                installed = {
                    name: (wireguard_dir / name).read_bytes() for name in server_files
                }
                self.assertEqual(installed, server_contents)
                for name in server_files:
                    self.assertEqual(
                        stat.S_IMODE((wireguard_dir / name).stat().st_mode), 0o600
                    )

                vpn.plan()
                vpn.install()
                self.assertEqual(
                    {
                        name: (wireguard_dir / name).read_bytes()
                        for name in server_files
                    },
                    installed,
                )
                vpn.uninstall()

            for name in server_files:
                self.assertFalse((wireguard_dir / name).exists())
            self.assertEqual(unrelated.read_text(), "unrelated config\n")
            self.assertEqual(stat.S_IMODE(unrelated.stat().st_mode), 0o600)
            with tarfile.open(output / "router.tar.gz", "r:gz") as bundle:
                inventory = bundle.getmember("router/network.json")
                self.assertEqual(inventory.mode, 0o600)
                self.assertEqual(
                    bundle.extractfile(inventory).read(),
                    (output / "router/network.json").read_bytes(),
                )
                self.assertEqual(bundle.getmember("router/forcad").mode, 0o700)
                self.assertEqual(
                    bundle.extractfile("router/forcad").read(4), b"\x7fELF"
                )
                self.assertEqual(bundle.getmember("router/network.nft").mode, 0o600)
                self.assertTrue(
                    bundle.extractfile("router/network.nft")
                    .read()
                    .startswith(b"create table inet forcad_monitoring\n")
                )

    def test_failed_metrics_build_removes_partial_output(self):
        for error in (
            ValueError("Install Go"),
            subprocess.CalledProcessError(1, ["go", "build"], stderr="build failed"),
            subprocess.CalledProcessError(
                1, ["forcad", "network-rules"], stderr="invalid inventory"
            ),
        ):
            with (
                self.subTest(error=str(error)),
                tempfile.TemporaryDirectory() as directory,
            ):
                output = Path(directory) / "runtime"
                with (
                    mock.patch.object(
                        sys,
                        "argv",
                        [
                            "render.py",
                            "--config",
                            str(ROOT / "settings.example.toml"),
                            "--output",
                            str(output),
                        ],
                    ),
                    mock.patch.object(
                        render, "build_router_metrics", side_effect=error
                    ),
                    mock.patch.object(render, "create_archives") as archives,
                    mock.patch("sys.stderr"),
                    self.assertRaises(SystemExit) as failure,
                ):
                    render.main()
                self.assertEqual(failure.exception.code, 1)
                self.assertFalse(output.exists())
                archives.assert_not_called()

    def test_failed_wireguard_generation_cleans_output_and_hides_subprocess_stderr(
        self,
    ):
        leaked_key = "PrivateKey = must-not-be-logged"
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "runtime"
            with (
                mock.patch.object(
                    sys,
                    "argv",
                    [
                        "render.py",
                        "--config",
                        str(ROOT / "settings.example.toml"),
                        "--output",
                        str(output),
                    ],
                ),
                mock.patch.object(render, "build_router_metrics"),
                mock.patch.object(render, "create_archives") as archives,
                mock.patch(
                    "vpn.generate.subprocess.run",
                    side_effect=subprocess.CalledProcessError(
                        1, ["wg", "genkey"], stderr=leaked_key
                    ),
                ),
                mock.patch("sys.stderr", new_callable=io.StringIO) as stderr,
                self.assertRaises(SystemExit) as failure,
            ):
                render.main()

            self.assertEqual(failure.exception.code, 1)
            self.assertFalse(output.exists())
            archives.assert_not_called()
            self.assertIn("WireGuard genkey failed", stderr.getvalue())
            self.assertNotIn(leaked_key, stderr.getvalue())

    def test_cross_compile_generates_rules_with_native_go(self):
        with tempfile.TemporaryDirectory() as directory:
            router = Path(directory) / "router"
            router.mkdir()
            (router / "network.json").write_text('{"teams": []}')
            calls = []

            def run(command, **kwargs):
                calls.append((command, dict(kwargs.get("env", {}))))
                if command[1] == "env":
                    return subprocess.CompletedProcess(
                        command, 0, '{"GOHOSTOS": "darwin", "GOHOSTARCH": "amd64"}'
                    )
                if command[1] == "build":
                    (router / "forcad").write_bytes(b"linux-arm64")
                    return subprocess.CompletedProcess(command, 0, "")
                self.assertEqual(command[1], "run")
                return subprocess.CompletedProcess(
                    command, 0, "create table inet forcad_monitoring\n"
                )

            with (
                mock.patch.object(render.shutil, "which", return_value="/usr/bin/go"),
                mock.patch.object(render.subprocess, "run", side_effect=run),
            ):
                render.build_router_metrics(directory, "arm64")
            self.assertEqual(calls[1][1]["GOOS"], "linux")
            self.assertEqual(calls[1][1]["GOARCH"], "arm64")
            self.assertEqual(calls[1][1]["CGO_ENABLED"], "0")
            self.assertEqual(calls[2][1]["GOOS"], "darwin")
            self.assertEqual(calls[2][1]["GOARCH"], "amd64")
            self.assertEqual((router / "forcad").read_bytes(), b"linux-arm64")
            self.assertEqual(
                (router / "network.nft").read_text(),
                "create table inet forcad_monitoring\n",
            )

    def test_render_has_no_secrets_and_supports_udp_only_game(self):
        self.config["env"]["monitoring"] = True
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
        self.assertIn(
            "./control.sh monitoring down -v --remove-orphans",
            files["forcad/install.sh"],
        )
        self.assertIn(
            "./control.sh down -v --remove-orphans", files["forcad/install.sh"]
        )
        self.assertIn(
            "label=com.docker.compose.project=forcad", files["forcad/install.sh"]
        )
        self.assertIn("docker volume rm", files["forcad/install.sh"])
        self.assertIn(".forcad-managed-packages", files["forcad/install.sh"])
        self.assertIn("down -v --remove-orphans", files["forcad/uninstall.sh"])
        self.assertIn('rm -rf -- "$target"', files["forcad/uninstall.sh"])
        self.assertIn("apt-get purge -y", files["forcad/uninstall.sh"])
        self.assertIn("--discard-backups", files["forcad/uninstall.sh"])
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
            for command in ("apt-get", "systemctl", "rmdir", "docker"):
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
                input="y\n",
                env=environment,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertFalse(target.exists())
            calls = call_log.read_text()
            self.assertIn("apt-get purge -y -- forcad-example", calls)
            self.assertIn("systemctl enable docker.service", calls)
            self.assertIn("systemctl start docker.service", calls)
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

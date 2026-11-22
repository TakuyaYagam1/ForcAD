"""VPN observer installation and packet/endpoint invariants."""

import base64
import json
import tempfile
import unittest
from pathlib import Path

from helpers import small_config, source_bundle

import render


class VpnObserverTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        work = Path(self.scratch.name)
        self.root = work / "host"
        self.root.mkdir()
        self.bundle_dir = work / "bundle"
        config = render.load_config(Path(render.__file__).parent / "settings.example.toml")
        small_config(config)
        render.write_bundle(self.bundle_dir, render.render(config))
        self.server_dir = self.bundle_dir / "vpn/server"
        self.server_dir.mkdir()

    def make_bundle(self, install_base=False):
        bundle = source_bundle("vpn", self.bundle_dir / "vpn", self.root)
        bundle.files_only = True
        for name, expected in bundle.config["wireguard"].items():
            private = base64.b64encode(bytes(range(32))).decode()
            content = (
                f"[Interface]\nPrivateKey = {private}\n"
                f"Address = {expected['address']}\nListenPort = {expected['port']}\n"
            )
            for number, peer in enumerate(expected["peers"]):
                public = base64.b64encode(bytes([number + 1]) * 32).decode()
                content += f"\n[Peer]\nPublicKey = {public}\nAllowedIPs = {peer}\n"
            (self.server_dir / f"{name}.conf").write_text(content)
        if install_base:
            bundle.plan()
            bundle.install()
        return bundle

    def test_disabled_observers_are_a_noop_without_a_base_receipt(self):
        bundle = self.make_bundle()

        bundle.install_observers(pcap=False, wg_endpoints=False)

        self.assertFalse(bundle.manifest.exists())
        self.assertFalse((self.root / "etc/systemd/system").exists())

    def test_enabled_observer_install_requires_base_receipt(self):
        bundle = self.make_bundle()

        with self.assertRaisesRegex(ValueError, "base VPN"):
            bundle.install_observers(pcap=True, wg_endpoints=False)

        self.assertFalse((self.root / "etc/systemd/system/forcad-vpn-pcap.service").exists())

    def test_observer_install_preserves_base_receipt_options(self):
        bundle = self.make_bundle()
        bundle.options["observer-test-option"] = "preserve"
        bundle.plan()
        bundle.install()
        options = json.loads(bundle.manifest.read_text())["options"]

        bundle.install_observers(pcap=True, wg_endpoints=False)

        self.assertEqual(
            json.loads(bundle.manifest.read_text())["options"],
            options,
        )

    def test_pcap_install_stages_only_manual_bounded_capture(self):
        bundle = self.make_bundle(install_base=True)
        network_config = self.root / "etc/wireguard/wg-attack.conf"
        before = network_config.read_bytes()

        bundle.install_observers(pcap=True, wg_endpoints=False)

        unit = (self.root / "etc/systemd/system/forcad-vpn-pcap.service").read_text()
        script = (self.root / "usr/lib/forcad/vpn/vpnlib/observers.py").read_text()
        self.assertIn("pcap-capture", unit)
        self.assertIn("/usr/bin/dumpcap", script)
        self.assertIn("UMask=0077", unit)
        self.assertIn("Environment=TZ=UTC", unit)
        self.assertIn("/var/lib/forcad-vpn-pcap", unit)
        self.assertNotIn("[Install]", unit)
        self.assertLess(unit.index("StartLimitIntervalSec="), unit.index("[Service]"))
        self.assertNotIn("forcad-wg-endpoints.service", unit)
        self.assertFalse((self.root / "etc/systemd/system/forcad-wg-endpoints.service").exists())
        self.assertEqual(network_config.read_bytes(), before)

    def test_endpoint_install_stages_socket_ui_without_pcap(self):
        bundle = self.make_bundle(install_base=True)

        bundle.install_observers(pcap=False, wg_endpoints=True)

        unit = (self.root / "etc/systemd/system/forcad-wg-endpoints.service").read_text()
        script = (self.root / "usr/lib/forcad/vpn/vpnlib/observers.py").read_text()
        socket_unit = (self.root / "etc/systemd/system/forcad-wg-history.socket").read_text()
        history_unit = (self.root / "etc/systemd/system/forcad-wg-history.service").read_text()
        history_script = self.root / "usr/lib/forcad/vpn/vpnlib/history.py"
        self.assertIn('["/usr/bin/wg", "show", "wg-attack", "allowed-ips"]', script)
        self.assertIn("/var/lib/forcad-wg-endpoints/history.sqlite3", unit)
        self.assertIn("Wants=forcad-wg-history.socket", unit)
        self.assertIn(
            "ExecStart=/usr/bin/python3 /usr/lib/forcad/vpn/vpnlib/history.py "
            "/var/lib/forcad-wg-endpoints/history.sqlite3",
            history_unit,
        )
        self.assertIn("PrivateNetwork=yes", history_unit)
        self.assertIn("RestrictAddressFamilies=AF_UNIX", history_unit)
        self.assertTrue(history_script.is_file())
        self.assertIn("/run/forcad-wg-history.sock", socket_unit)
        self.assertIn("SocketGroup=adm", socket_unit)
        self.assertIn("SocketMode=0660", socket_unit)
        self.assertNotIn("[Install]", socket_unit)
        self.assertFalse((self.root / "etc/systemd/system/forcad-vpn-pcap.service").exists())

    def test_unowned_observer_unit_is_not_replaced(self):
        bundle = self.make_bundle(install_base=True)
        path = self.root / "etc/systemd/system/forcad-vpn-pcap.service"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("administrator-owned unit\n")
        manifest_before = bundle.manifest.read_bytes()

        with self.assertRaisesRegex(ValueError, "not owned"):
            bundle.install_observers(pcap=True, wg_endpoints=False)

        self.assertEqual(path.read_text(), "administrator-owned unit\n")
        self.assertEqual(bundle.manifest.read_bytes(), manifest_before)

    def test_sequential_feature_installs_preserve_prior_receipt_entries(self):
        bundle = self.make_bundle(install_base=True)

        bundle.install_observers(pcap=True, wg_endpoints=False)
        bundle.install_observers(pcap=False, wg_endpoints=True)
        receipt = json.loads(bundle.manifest.read_text())
        paths = {item["path"] for item in receipt["files"]}

        self.assertIn("/etc/systemd/system/forcad-vpn-pcap.service", paths)
        self.assertIn("/etc/systemd/system/forcad-wg-endpoints.service", paths)
        self.assertIn("/etc/systemd/system/forcad-wg-history.socket", paths)

    def test_base_reinstall_keeps_installed_observers_owned(self):
        bundle = self.make_bundle(install_base=True)
        bundle.install_observers(pcap=True, wg_endpoints=False)
        bundle.plan()

        bundle.install()

        receipt = json.loads(bundle.manifest.read_text())
        paths = {item["path"] for item in receipt["files"]}
        self.assertIn("/etc/systemd/system/forcad-vpn-pcap.service", paths)
        self.assertTrue((self.root / "etc/systemd/system/forcad-vpn-pcap.service").is_file())

    def test_uninstall_removes_observer_units_and_keeps_capture_data(self):
        bundle = self.make_bundle(install_base=True)
        bundle.install_observers(pcap=True, wg_endpoints=True)
        evidence = self.root / "var/lib/forcad-vpn-pcap/capture.pcap"
        evidence.parent.mkdir(parents=True)
        evidence.write_bytes(b"kept evidence")

        bundle.uninstall()

        self.assertFalse((self.root / "etc/systemd/system/forcad-vpn-pcap.service").exists())
        self.assertFalse((self.root / "etc/systemd/system/forcad-wg-endpoints.service").exists())
        self.assertEqual(evidence.read_bytes(), b"kept evidence")


class VpnObserverDataTests(unittest.TestCase):
    def test_capture_filter_is_exact_bidirectional_inventory_pairing(self):
        from vpn.vpnlib.observers import build_capture_filter

        teams = [
            {"name": "alpha", "vulnbox": "10.20.0.2", "peers": ["10.30.0.2"]},
            {"name": "NPC", "vulnbox": "10.21.0.2", "peers": []},
        ]

        capture_filter = build_capture_filter(teams)

        self.assertIn("src host 10.30.0.2", capture_filter)
        self.assertIn("dst host 10.20.0.2", capture_filter)
        self.assertIn("dst host 10.21.0.2", capture_filter)
        self.assertIn("src host 10.20.0.2", capture_filter)
        self.assertNotIn("10.30.0.0/24", capture_filter)
        with self.assertRaises(ValueError):
            build_capture_filter(
                [{"name": "bad", "vulnbox": "10.20.0.2 or port 22", "peers": []}]
            )

    def test_pcap_capture_executes_classic_utc_bounded_ring_command(self):
        from types import SimpleNamespace
        from unittest.mock import patch

        from vpn.vpnlib.observers import pcap_capture

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            inventory = root / "inventory.json"
            inventory.write_text(
                json.dumps(
                    {
                        "teams": [
                            {
                                "name": "alpha",
                                "vulnbox": "10.20.0.2",
                                "peers": ["10.30.0.2"],
                            }
                        ]
                    }
                )
            )
            captures = root / "captures"
            with (
                patch(
                    "vpn.vpnlib.observers.shutil.disk_usage",
                    return_value=SimpleNamespace(free=10**12),
                ),
                patch("vpn.vpnlib.observers.os.execv") as execv,
            ):
                pcap_capture(inventory, captures)

        executable, command = execv.call_args.args
        self.assertEqual(executable, "/usr/bin/dumpcap")
        self.assertIn("interval:300", command)
        self.assertIn("filesize:4000000", command)
        self.assertIn("files:96", command)
        self.assertIn("262144", command)
        self.assertEqual(command[command.index("-F") + 1], "pcap")

    def test_wireguard_snapshot_uses_exact_authenticated_inventory_and_venue_cidrs(self):
        from vpn.vpnlib.observers import parse_venue_cidrs, parse_wireguard_snapshot

        teams = [{"name": "alpha", "vulnbox": "10.20.0.2", "peers": ["10.30.0.2"]}]
        snapshot = parse_wireguard_snapshot(
            teams,
            "peer-a\t10.30.0.2/32\npeer-b\t10.30.0.3/24\npeer-c\t10.30.0.4/32\n",
            "peer-a\t198.51.100.8:40000\npeer-b\t198.51.100.9:40000\npeer-c\t203.0.113.9:50000\n",
            "peer-a\t1770000000\npeer-b\t1770000000\npeer-c\t1770000000\n",
            parse_venue_cidrs(["198.51.100.0/24", "203.0.113.0/24"]),
            observed_at=1770000001,
        )

        self.assertEqual(
            snapshot,
            {
                "10.30.0.2": {
                    "team_name": "alpha",
                    "member": "member-01",
                    "external_ip": "198.51.100.8",
                    "venue": "venue",
                    "authenticated_recently": True,
                }
            },
        )

    def test_stale_wireguard_handshake_is_not_a_current_endpoint(self):
        from vpn.vpnlib.observers import parse_wireguard_snapshot

        snapshot = parse_wireguard_snapshot(
            [{"name": "alpha", "vulnbox": "10.20.0.2", "peers": ["10.30.0.2"]}],
            "peer-a\t10.30.0.2/32\n",
            "peer-a\t198.51.100.8:40000\n",
            "peer-a\t1770000000\n",
            observed_at=1770000181,
        )

        self.assertEqual(snapshot, {})

    def test_endpoint_bootstrap_and_active_changes_ignore_port_rebinding(self):
        import sqlite3

        from vpn.vpnlib.observers import record_observations

        connection = sqlite3.connect(":memory:")
        self.addCleanup(connection.close)
        connection.execute(
            "CREATE TABLE observations (id INTEGER PRIMARY KEY, observed_at INTEGER, "
            "source_ip TEXT, team_name TEXT, member TEXT, external_ip TEXT, "
            "venue TEXT, kind TEXT)"
        )
        initial = {
            "10.30.0.2": {
                "team_name": "alpha",
                "member": "member-01",
                "external_ip": "198.51.100.8",
                "venue": "unknown",
                "authenticated_recently": True,
            }
        }

        state = record_observations(connection, {}, initial, bootstrap=True, observed_at=10)
        state = record_observations(connection, state, initial, bootstrap=True, observed_at=11)
        rebound = {"10.30.0.2": {**initial["10.30.0.2"], "port": 50000}}
        state = record_observations(connection, state, rebound, bootstrap=False, observed_at=15)
        moved = {"10.30.0.2": {**initial["10.30.0.2"], "external_ip": "203.0.113.8"}}
        state = record_observations(connection, state, moved, bootstrap=False, observed_at=20)
        rows = connection.execute(
            "SELECT observed_at, external_ip, kind FROM observations ORDER BY id"
        ).fetchall()

        self.assertEqual(
            rows,
            [
                (10, "198.51.100.8", "baseline"),
                (11, "198.51.100.8", "baseline"),
                (20, "203.0.113.8", "change"),
            ],
        )
        self.assertEqual(state["10.30.0.2"], "203.0.113.8")

    def test_active_stable_endpoint_gets_a_bounded_fresh_baseline(self):
        import sqlite3

        from vpn.vpnlib.observers import record_observations

        connection = sqlite3.connect(":memory:")
        self.addCleanup(connection.close)
        connection.execute(
            "CREATE TABLE observations (id INTEGER PRIMARY KEY, observed_at INTEGER, "
            "source_ip TEXT, team_name TEXT, member TEXT, external_ip TEXT, "
            "venue TEXT, kind TEXT)"
        )
        active = {
            "10.30.0.2": {
                "team_name": "alpha",
                "member": "member-01",
                "external_ip": "198.51.100.8",
                "venue": "unknown",
                "authenticated_recently": True,
            }
        }

        state = record_observations(connection, {}, active, bootstrap=True, observed_at=100)
        state = record_observations(
            connection, state, active, bootstrap=False, observed_at=100 + 86_399
        )
        state = record_observations(
            connection, state, active, bootstrap=False, observed_at=100 + 86_400
        )
        record_observations(
            connection, state, active, bootstrap=False, observed_at=100 + 86_401
        )

        rows = connection.execute(
            "SELECT observed_at, kind FROM observations ORDER BY id"
        ).fetchall()
        self.assertEqual(rows, [(100, "baseline"), (86_500, "baseline")])

    def test_capture_file_selection_spans_adjacent_utc_segments(self):
        from datetime import datetime, timezone

        from vpn.vpnlib.observers import select_capture_files

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            names = (
                "capture_1_00001_20261122000000.pcap",
                "capture_1_00002_20261122000500.pcap",
                "capture_1_00003_20261122001000.pcap",
            )
            files = [root / name for name in names]
            for path in files:
                path.write_bytes(b"pcap")
            start = int(datetime(2026, 11, 22, 0, 4, tzinfo=timezone.utc).timestamp() * 1000)
            end = int(datetime(2026, 11, 22, 0, 6, tzinfo=timezone.utc).timestamp() * 1000)

            selected = select_capture_files(root, start, end)

        self.assertEqual(selected, files[:2])

    def test_capture_file_selection_keeps_same_second_rotations(self):
        from datetime import datetime, timezone

        from vpn.vpnlib.observers import select_capture_files

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            names = (
                "capture_1_00001_20261122000001.pcap",
                "capture_1_00002_20261122000001.pcap",
                "capture_1_00003_20261122000002.pcap",
                "capture_1_00004_20261122000002.pcap",
                "capture_1_00005_20261122000003.pcap",
            )
            files = [root / name for name in names]
            for path in files:
                path.write_bytes(b"pcap")
            start = int(
                datetime(2026, 11, 22, 0, 0, 2, 500_000, tzinfo=timezone.utc).timestamp()
                * 1000
            )
            end = start + 400

            selected = select_capture_files(root, start, end)

        self.assertEqual(selected, files[:4])

    def test_flow_filter_is_typed_bidirectional_and_time_bounded(self):
        from vpn.vpnlib.observers import build_flow_filter

        expression = build_flow_filter(
            "tcp", "10.30.0.2", 49152, "10.20.0.2", 22, 1795310000123, 1795310000999
        )

        self.assertIn("tcp.srcport == 49152", expression)
        self.assertIn("tcp.dstport == 22", expression)
        self.assertIn("tcp.srcport == 22", expression)
        self.assertIn("tcp.dstport == 49152", expression)
        self.assertIn("frame.time_epoch >= 1795310000.123", expression)
        self.assertIn("frame.time_epoch <= 1795310000.999", expression)
        with self.assertRaises(ValueError):
            build_flow_filter("tcp", "10.30.0.2 or tcp", 1, "10.20.0.2", 22, 1, 2)

    def test_ring_pruning_bounds_restarts_and_ignores_unowned_files(self):
        from vpn.vpnlib.observers import (
            MAX_CAPTURE_FILES,
            RING_CAPTURE_FILES,
            prune_capture_files,
        )

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            captures = [
                root / f"capture_1_{number:05d}_20261122{number:06d}.pcap"
                for number in range(MAX_CAPTURE_FILES + 5)
            ]
            for path in captures:
                path.write_bytes(b"pcap")
            unrelated = root / "keep.txt"
            unrelated.write_text("do not delete")

            prune_capture_files(root)

            remaining = list(root.glob("capture_*.pcap"))
            self.assertLessEqual(len(remaining), MAX_CAPTURE_FILES - RING_CAPTURE_FILES)
            self.assertTrue(unrelated.exists())
            self.assertTrue(all(path.exists() for path in captures[-(MAX_CAPTURE_FILES - RING_CAPTURE_FILES) :]))


    def test_default_capture_ring_covers_six_hours_at_100_mbit_stored_per_second(self):
        from vpn.vpnlib.observers import (
            CAPTURE_FILE_KB,
            CAPTURE_FILE_LIMIT_BYTES,
            CAPTURE_INTERVAL_SECONDS,
            CAPTURE_STORAGE_LIMIT_BYTES,
            MAX_CAPTURE_FILES,
            RING_CAPTURE_FILES,
        )

        # Both limits apply: a size rotation does not cancel the wall-clock rotation.
        bytes_per_interval = (100_000_000 // 8) * CAPTURE_INTERVAL_SECONDS
        self.assertGreater(CAPTURE_FILE_KB * 1000, bytes_per_interval)
        self.assertGreaterEqual(RING_CAPTURE_FILES * CAPTURE_INTERVAL_SECONDS, 6 * 60 * 60)
        self.assertEqual(MAX_CAPTURE_FILES, RING_CAPTURE_FILES * 2)
        self.assertEqual(
            CAPTURE_STORAGE_LIMIT_BYTES,
            MAX_CAPTURE_FILES * CAPTURE_FILE_LIMIT_BYTES,
        )

    def test_capture_preflight_failure_preserves_historic_segments(self):
        from types import SimpleNamespace
        from unittest.mock import patch

        from vpn.vpnlib.observers import pcap_capture

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            inventory = root / "inventory.json"
            inventory.write_text(
                json.dumps(
                    {
                        "teams": [
                            {
                                "name": "alpha",
                                "vulnbox": "10.20.0.2",
                                "peers": ["10.30.0.2"],
                            }
                        ]
                    }
                )
            )
            old = root / "capture_1_00001_20261122000000.pcap"
            old.write_bytes(b"historic evidence")
            with patch(
                "vpn.vpnlib.observers.shutil.disk_usage",
                return_value=SimpleNamespace(free=0),
            ):
                with self.assertRaisesRegex(ValueError, "insufficient free space"):
                    pcap_capture(inventory, root)

            self.assertEqual(old.read_bytes(), b"historic evidence")

    def test_empty_flow_result_is_not_published(self):
        import io
        from types import SimpleNamespace
        from unittest.mock import patch

        from vpn.vpnlib.observers import extract_flow

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            capture = root / "capture_1_00001_20261122000000.pcap"
            capture.write_bytes(b"capture")
            mergecap = root / "mergecap"
            tshark = root / "tshark"
            mergecap.touch()
            tshark.touch()

            class FakeProcess:
                def __init__(self, command, **kwargs):
                    self.returncode = 0
                    self.stdout = io.BytesIO() if command[0] == str(mergecap) else None
                    if command[0] == str(tshark):
                        kwargs["stdout"].write(b"\0" * 24)

                def wait(self, *args, **kwargs):
                    return 0

                def poll(self):
                    return self.returncode

                def terminate(self):
                    self.returncode = -1

                def kill(self):
                    self.returncode = -9

            with (
                patch(
                    "vpn.vpnlib.observers.subprocess.Popen", side_effect=FakeProcess
                ) as popen,
                patch(
                    "vpn.vpnlib.observers.shutil.disk_usage",
                    return_value=SimpleNamespace(free=11 * 1024**3),
                ),
            ):
                with self.assertRaisesRegex(ValueError, "no packets matched"):
                    extract_flow(
                        root,
                        "udp",
                        "10.30.0.2",
                        49152,
                        "10.20.0.2",
                        53,
                        1795310000123,
                        1795310000999,
                        "empty",
                        mergecap=str(mergecap),
                        tshark=str(tshark),
                    )
            tshark_args = popen.call_args_list[1].args[0]
            self.assertIn("-n", tshark_args)
            self.assertIn("-M", tshark_args)
            self.assertEqual(tshark_args[-2:], ["-w", "-"])
            self.assertTrue(popen.call_args_list[1].kwargs["preexec_fn"])
            self.assertFalse(list((root / "exports").glob(".partial-*.pcap")))
            self.assertFalse(list((root / "exports").glob("flow-*.pcap")))

    def test_export_worker_drops_root_privileges_but_keeps_nonroot_identity(self):
        import resource
        from types import SimpleNamespace
        from unittest.mock import patch

        from vpn.vpnlib.observers import MAX_EXPORT_FILE_BYTES, _export_child_setup

        with (
            patch("vpn.vpnlib.observers.os.geteuid", return_value=0),
            patch(
                "vpn.vpnlib.observers.pwd.getpwnam",
                return_value=SimpleNamespace(pw_uid=65534, pw_gid=65534),
            ),
            patch("vpn.vpnlib.observers.resource.setrlimit") as setrlimit,
            patch("vpn.vpnlib.observers.os.setgroups") as setgroups,
            patch("vpn.vpnlib.observers.os.setgid") as setgid,
            patch("vpn.vpnlib.observers.os.setuid") as setuid,
        ):
            _export_child_setup()

        setrlimit.assert_called_once_with(
            resource.RLIMIT_FSIZE, (MAX_EXPORT_FILE_BYTES, MAX_EXPORT_FILE_BYTES)
        )
        setgroups.assert_called_once_with([])
        setgid.assert_called_once_with(65534)
        setuid.assert_called_once_with(65534)

        with (
            patch("vpn.vpnlib.observers.os.geteuid", return_value=1000),
            patch("vpn.vpnlib.observers.pwd.getpwnam") as getpwnam,
            patch("vpn.vpnlib.observers.resource.setrlimit") as setrlimit,
            patch("vpn.vpnlib.observers.os.setgroups") as setgroups,
            patch("vpn.vpnlib.observers.os.setgid") as setgid,
            patch("vpn.vpnlib.observers.os.setuid") as setuid,
        ):
            _export_child_setup()

        setrlimit.assert_called_once()
        setgroups.assert_not_called()
        setgid.assert_not_called()
        setuid.assert_not_called()
        getpwnam.assert_not_called()
if __name__ == "__main__":
    unittest.main()

"""VPN runtime install and uninstall behavior with an isolated host root."""

import base64
import json
import stat
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path
from unittest.mock import Mock, call, patch

from helpers import small_config, source_bundle

import render


class VpnLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        work = Path(self.scratch.name)
        self.root = work / "host"
        self.root.mkdir()
        self.bundle_dir = work / "bundle"
        config = render.load_config(
            Path(render.__file__).parent / "settings.example.toml"
        )
        small_config(config)
        render.write_bundle(self.bundle_dir, render.render(config))
        server = self.bundle_dir / "vpn/server"
        server.mkdir()
        self.original = self.root / "etc/network/interfaces"
        self.original.parent.mkdir(parents=True)
        self.original.write_text("auto lo\niface lo inet loopback\n")
        self.make()

    def make(self):
        bundle = source_bundle("vpn", self.bundle_dir / "vpn", self.root)
        bundle.files_only = False
        server = self.bundle_dir / "vpn/server"
        for name, expected in bundle.config["wireguard"].items():
            private = base64.b64encode(bytes(range(32))).decode()
            content = (
                f"[Interface]\nPrivateKey = {private}\n"
                f"Address = {expected['address']}\nListenPort = {expected['port']}\n"
            )
            for number, peer in enumerate(expected["peers"]):
                public = base64.b64encode(bytes([number + 1]) * 32).decode()
                content += f"\n[Peer]\nPublicKey = {public}\nAllowedIPs = {peer}\n"
            (server / f"{name}.conf").write_text(content)
        bundle.plan()
        return bundle

    @staticmethod
    def snapshots():
        before = {
            "role": "vpn",
            "units": {"baseline": "old"},
            "table": None,
            "sysctls": {"net.ipv4.ip_forward": "0"},
            "network": {"addresses": ["old"]},
        }
        applied = {
            "role": "vpn",
            "units": {"baseline": "running"},
            "table": "forcad_vpn",
            "sysctls": {"net.ipv4.ip_forward": "1"},
            "network": {"addresses": ["installed"]},
        }
        return before, applied

    @staticmethod
    def runtime_snapshots(preexisting_wireguard=False):
        from vpnlib.lifecycle import (
            FIREWALL,
            FIREWALL_MANAGERS,
            NETWORKING,
            NFTABLES,
            WG_UNITS,
        )

        before, applied = VpnLifecycleTests.snapshots()
        inactive = {
            "load_state": "loaded",
            "active_state": "inactive",
            "unit_file_state": "disabled",
        }
        before["units"] = {
            NETWORKING: {
                "load_state": "loaded",
                "active_state": "active",
                "unit_file_state": "enabled",
            },
            NFTABLES: deepcopy(inactive),
            **{unit: deepcopy(inactive) for unit in FIREWALL_MANAGERS},
            FIREWALL: {
                "load_state": "not-found",
                "active_state": "inactive",
                "unit_file_state": "",
            },
            **{
                unit: {
                    "load_state": "loaded",
                    "active_state": ("active" if preexisting_wireguard else "inactive"),
                    "unit_file_state": (
                        "enabled" if preexisting_wireguard else "disabled"
                    ),
                }
                for unit in WG_UNITS
            },
        }
        before["table"] = None
        before["wireguard_interfaces"] = []

        applied["units"] = deepcopy(before["units"])
        applied["units"][FIREWALL] = {
            "load_state": "loaded",
            "active_state": "active",
            "unit_file_state": "enabled",
        }
        for unit in WG_UNITS:
            applied["units"][unit] = {
                "load_state": "loaded",
                "active_state": "active",
                "unit_file_state": "enabled",
            }
        applied["table"] = "table inet forcad_vpn {}"
        applied["wireguard_interfaces"] = ["wg-admin", "wg-attack"]
        return before, applied

    @staticmethod
    def adopt_wireguard_configs(bundle):
        adopted = {}
        for name in ("wg-attack", "wg-admin"):
            path = bundle.path(f"/etc/wireguard/{name}.conf")
            path.parent.mkdir(parents=True, exist_ok=True)
            data = bundle.changes[path][0]
            path.write_bytes(data)
            path.chmod(0o600)
            adopted[path] = (data, stat.S_IMODE(path.stat().st_mode))
        return adopted

    def bind_runtime(self, bundle, snapshots):
        from vpnlib.lifecycle import Lifecycle

        lifecycle = Lifecycle(bundle)
        runtime = Mock()
        runtime.snapshot.side_effect = snapshots
        lifecycle.runtime = runtime
        patcher = patch("vpn.host.Lifecycle", return_value=lifecycle)
        patcher.start()
        self.addCleanup(patcher.stop)
        return lifecycle, runtime

    def test_install_applies_runtime_and_uninstall_restores_it(self):
        bundle = self.make()
        before, applied = self.snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])

        bundle.install()

        runtime.preflight.assert_called_once_with(before, allow_existing_table=False)
        runtime.apply.assert_called_once_with(before)
        self.assertEqual(lifecycle.load()["baseline"], before)
        self.assertTrue(bundle.manifest.exists())

        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = applied
        bundle.uninstall()

        runtime.stop.assert_called_once()
        runtime.restore.assert_called_once_with(lifecycle.removal_snapshot(before))
        self.assertFalse(bundle.manifest.exists())
        self.assertIsNone(lifecycle.load())

    def test_runtime_start_failure_restores_preexisting_staged_files(self):
        bundle = self.make()
        bundle.files_only = True
        bundle.install()
        installed_interfaces = bundle.path("/etc/network/interfaces").read_bytes()
        manifest = bundle.manifest.read_bytes()
        bundle.files_only = False
        before, _ = self.runtime_snapshots(preexisting_wireguard=True)
        lifecycle, runtime = self.bind_runtime(bundle, [before])
        runtime.apply.side_effect = ValueError("tunnel failed")

        with self.assertRaisesRegex(ValueError, "files and live state restored"):
            bundle.install()

        self.assertEqual(
            bundle.path("/etc/network/interfaces").read_bytes(), installed_interfaces
        )
        self.assertEqual(bundle.manifest.read_bytes(), manifest)
        runtime.restore.assert_called_once_with(before)
        self.assertIsNone(lifecycle.load())

    def test_identical_adopted_configs_are_planned_and_removed(self):
        bundle = self.make()
        adopted = self.adopt_wireguard_configs(bundle)
        before, applied = self.runtime_snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])

        bundle.install()

        installed = json.loads(bundle.manifest.read_text())
        backup = bundle.path(installed["rollback"]["backup"])
        backed_up = {
            item["path"] for item in json.loads((backup / "files.json").read_text())
        }
        plan = bundle.preflight_uninstall()
        actions = {item["path"]: item["action"] for item in plan["actions"]}
        for path in adopted:
            name = "/" + str(path.relative_to(self.root))
            self.assertNotIn(name, backed_up)
            self.assertEqual(actions[name], "remove")
            self.assertEqual(adopted[path][1], 0o600)

        manifest_before = bundle.manifest.read_bytes()
        journal_before = lifecycle.journal.read_bytes()
        dry_state = {
            path: (path.read_bytes(), stat.S_IMODE(path.stat().st_mode))
            for path in adopted
        }
        bundle.uninstall(dry_run=True)
        self.assertEqual(
            {
                path: (path.read_bytes(), stat.S_IMODE(path.stat().st_mode))
                for path in adopted
            },
            dry_state,
        )
        self.assertEqual(bundle.manifest.read_bytes(), manifest_before)
        self.assertEqual(lifecycle.journal.read_bytes(), journal_before)

        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = applied
        bundle.uninstall()

        for path in adopted:
            self.assertFalse(path.exists())
        self.assertFalse((self.root / "etc/wireguard").exists())

    def test_removal_snapshot_disables_managed_vpn_and_keeps_shared_state(self):
        from vpnlib.lifecycle import FIREWALL, WG_UNITS, Lifecycle

        before, _ = self.runtime_snapshots(preexisting_wireguard=True)
        before["units"][FIREWALL] = {
            "load_state": "loaded",
            "active_state": "active",
            "unit_file_state": "enabled",
        }
        before["table"] = "table inet forcad_vpn {}"
        before["wireguard_interfaces"] = ["wg-admin", "wg-attack", "wg-office"]

        target = Lifecycle.removal_snapshot(before)

        self.assertIsNot(target, before)
        self.assertIsNot(target["units"], before["units"])
        self.assertIsNot(target["network"], before["network"])
        for unit in WG_UNITS:
            self.assertEqual(target["units"][unit]["active_state"], "inactive")
            self.assertEqual(target["units"][unit]["unit_file_state"], "disabled")
        self.assertEqual(
            target["units"][FIREWALL],
            {
                "load_state": "not-found",
                "active_state": "inactive",
                "unit_file_state": "",
            },
        )
        self.assertIsNone(target["table"])
        self.assertEqual(target["wireguard_interfaces"], ["wg-office"])
        self.assertEqual(target["network"], before["network"])
        self.assertEqual(target["sysctls"], before["sysctls"])
        self.assertEqual(
            target["units"]["nftables.service"], before["units"]["nftables.service"]
        )
        self.assertEqual(before["units"][FIREWALL]["active_state"], "active")
        self.assertEqual(before["table"], "table inet forcad_vpn {}")
        self.assertEqual(
            before["wireguard_interfaces"], ["wg-admin", "wg-attack", "wg-office"]
        )

    def test_uninstall_restores_normalized_wireguard_baseline(self):
        bundle = self.make()
        before, applied = self.runtime_snapshots(preexisting_wireguard=True)
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])

        bundle.install()
        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = applied
        bundle.uninstall()

        target = lifecycle.removal_snapshot(before)
        runtime.restore.assert_called_once_with(target)
        runtime.verify_removed.assert_called_once_with()
        method_names = [item[0] for item in runtime.method_calls]
        self.assertLess(
            method_names.index("restore"), method_names.index("verify_removed")
        )
        for unit in target["units"]:
            if unit.startswith("wg-quick@"):
                self.assertEqual(before["units"][unit]["unit_file_state"], "enabled")

    def test_uninstall_preserves_unrelated_wireguard_config_and_directory(self):
        bundle = self.make()
        unrelated = self.root / "etc/wireguard/wg-office.conf"
        unrelated.parent.mkdir(parents=True)
        unrelated.write_text("[Interface]\nAddress = 192.0.2.4/32\n")
        unrelated.chmod(0o600)
        expected = unrelated.read_bytes()
        before, applied = self.runtime_snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])

        bundle.install()
        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = applied
        bundle.uninstall()

        self.assertEqual(unrelated.read_bytes(), expected)
        self.assertEqual(stat.S_IMODE(unrelated.stat().st_mode), 0o600)
        self.assertTrue(unrelated.parent.is_dir())
        lifecycle.runtime.verify_removed.assert_called_once_with()

    def test_uninstall_restore_failure_rolls_back_adopted_configs_and_journal(self):
        bundle = self.make()
        adopted = self.adopt_wireguard_configs(bundle)
        before, applied = self.runtime_snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])

        bundle.install()
        manifest_before = bundle.manifest.read_bytes()
        journal_before = lifecycle.journal.read_bytes()
        record_before = lifecycle.load()
        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = applied
        runtime.restore.side_effect = [RuntimeError("restore failed"), None]

        with self.assertRaisesRegex(RuntimeError, "restore failed"):
            bundle.uninstall()

        for path, (data, mode) in adopted.items():
            self.assertEqual(path.read_bytes(), data)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), mode)
        self.assertEqual(bundle.manifest.read_bytes(), manifest_before)
        self.assertEqual(lifecycle.journal.read_bytes(), journal_before)
        self.assertEqual(lifecycle.load(), record_before)
        self.assertEqual(runtime.restore.call_count, 2)

    def test_reinstall_uses_normalized_state_as_new_baseline(self):
        from vpnlib.lifecycle import Lifecycle

        bundle = self.make()
        before, applied = self.runtime_snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])

        bundle.install()
        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = applied
        bundle.uninstall()
        removed = Lifecycle.removal_snapshot(before)

        runtime.snapshot.side_effect = [removed, applied]
        bundle.install()

        self.assertEqual(lifecycle.load()["baseline"], removed)
        runtime.apply.assert_has_calls([call(before), call(removed)])

    def test_uninstall_without_manifest_keeps_unowned_empty_wireguard_directory(self):
        bundle = self.make()
        wireguard = self.root / "etc/wireguard"
        wireguard.mkdir(parents=True)
        bundle.files_only = True

        bundle.uninstall()

        self.assertTrue(wireguard.is_dir())
        self.assertEqual(list(wireguard.iterdir()), [])

    def test_verify_removed_rejects_reappeared_managed_tunnel(self):
        from vpnlib import lifecycle

        runtime = lifecycle.Runtime(self.make())
        with (
            patch.object(runtime, "_require_live_root"),
            patch.object(
                runtime,
                "_wireguard_interfaces",
                return_value=["wg-office", "wg-attack"],
            ),
            patch.object(runtime, "_unit_state") as unit_state,
            self.assertRaisesRegex(
                ValueError, "Managed WireGuard interfaces remain active: wg-attack"
            ),
        ):
            runtime.verify_removed()
        unit_state.assert_not_called()

    def test_uninstall_restores_state_after_services_were_stopped(self):
        bundle = self.make()
        before, applied = self.snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])
        bundle.install()
        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = {**applied, "units": before["units"]}
        bundle.uninstall()
        runtime.stop.assert_called_once()
        runtime.restore.assert_called_once_with(lifecycle.removal_snapshot(before))
        self.assertFalse(lifecycle.journal.parent.exists())

    def test_runtime_stop_removes_both_tunnels_and_only_its_firewall(self):
        from vpnlib import lifecycle

        runtime = lifecycle.Runtime(self.make())
        with (
            patch.object(runtime, "_require_live_root"),
            patch.object(
                runtime,
                "_unit_state",
                return_value={
                    "active_state": "active",
                    "unit_file_state": "enabled",
                },
            ),
            patch.object(
                runtime, "_wireguard_interfaces", return_value=["wg-admin", "wg-attack"]
            ),
            patch.object(runtime, "_table", return_value="table inet forcad_vpn {}"),
            patch.object(lifecycle, "stop_firewall") as stop_firewall,
            patch.object(lifecycle, "run") as run,
        ):
            runtime.stop()
        for name in ("wg-admin", "wg-attack"):
            run.assert_any_call("systemctl", "stop", f"wg-quick@{name}.service")
            run.assert_any_call("systemctl", "disable", f"wg-quick@{name}.service")
            run.assert_any_call("wg-quick", "down", name)
        run.assert_any_call("nft", "delete", "table", "inet", "forcad_vpn")
        stop_firewall.assert_called_once_with(runtime.bundle, lifecycle.FIREWALL)
        self.assertFalse(any("flush" in call.args for call in run.call_args_list))

    def test_restore_rejects_foreign_tables_before_changing_nftables_service(self):
        from vpnlib import lifecycle

        runtime = lifecycle.Runtime(self.make())
        snapshot = {"units": {lifecycle.NFTABLES: {"active_state": "active"}}}
        with (
            patch.object(
                runtime, "_unit_state", return_value={"active_state": "inactive"}
            ),
            patch.object(
                lifecycle,
                "run",
                return_value=json.dumps(
                    {
                        "nftables": [{"table": {"family": "inet", "name": "other"}}],
                    }
                ),
            ) as run,
            self.assertRaisesRegex(ValueError, "foreign tables"),
        ):
            runtime.check_restore(snapshot)
        run.assert_called_once_with("nft", "-j", "list", "tables")


if __name__ == "__main__":
    unittest.main()

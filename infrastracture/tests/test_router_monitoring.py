"""File-backed tests for additive router monitoring installation."""

import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from helpers import small_config, source_bundle

import render


class RouterMonitoringTests(unittest.TestCase):
    def setUp(self):
        scratch = tempfile.TemporaryDirectory()
        self.addCleanup(scratch.cleanup)
        work = Path(scratch.name)
        self.root = work / "host"
        self.root.mkdir()
        self.directory = work / "bundle"
        config = render.load_config(
            Path(render.__file__).parent / "settings.example.toml"
        )
        small_config(config)
        render.write_bundle(self.directory, render.render(config))
        (self.directory / "router/network.nft").write_text(
            "table inet forcad_monitoring {\n    chain input { counter }\n}\n"
        )
        # These tests exercise file ownership, not the separately built Go binary.
        (self.directory / "router/forcad").write_bytes(b"test exporter artifact")
        interfaces = self.root / "etc/network/interfaces"
        interfaces.parent.mkdir(parents=True)
        interfaces.write_text("auto lo\niface lo inet loopback\n")

        self.bundle = source_bundle("router", self.directory / "router", self.root)
        self.bundle.files_only = False
        self.bundle.plan()

        self.network = Mock()
        self.network_state = {"addresses": ["original"]}
        self.network.snapshot.side_effect = lambda **kwargs: copy.deepcopy(
            self.network_state
        )
        self.system = Mock()
        self.monitoring_active = False
        self.system.snapshot.side_effect = self.snapshot
        self.system.apply_monitoring.side_effect = self.apply_monitoring
        for name, adapter in (("Network", self.network), ("System", self.system)):
            patcher = patch(f"routerlib.lifecycle.{name}", return_value=adapter)
            patcher.start()
            self.addCleanup(patcher.stop)

        from routerlib.lifecycle import Lifecycle

        self.Lifecycle = Lifecycle

    def system_snapshot(self, options):
        active = self.monitoring_active
        units = {
            "forcad-router-firewall.service": {
                "load_state": "loaded",
                "active_state": "active",
                "unit_file_state": "enabled",
            },
            "networking.service": {
                "load_state": "loaded",
                "active_state": "active",
                "unit_file_state": "enabled",
            },
        }
        tables = {
            "inet forcad_router": "table inet forcad_router { }",
            "ip forcad_nat": "table ip forcad_nat { }",
        }
        inventory = ["inet forcad_router", "ip forcad_nat"]
        if options.get("network_metrics"):
            state = "loaded" if active else "not-found"
            file_state = "enabled" if active else "not-found"
            active_state = "active" if active else "inactive"
            for unit in (
                "network-rules.service",
                "network-metrics.service",
            ):
                units[unit] = {
                    "load_state": state,
                    "active_state": active_state,
                    "unit_file_state": file_state,
                }
            tables["inet forcad_monitoring"] = (
                "table inet forcad_monitoring { }" if active else None
            )
            if active:
                inventory.append("inet forcad_monitoring")
        if options.get("game_metrics"):
            units["game-metrics.service"] = {
                "load_state": "loaded" if active else "not-found",
                "active_state": "active" if active else "inactive",
                "unit_file_state": "enabled" if active else "not-found",
            }
        return {
            "role": "router",
            "options": {
                "network_metrics": bool(options.get("network_metrics")),
                "game_metrics": bool(options.get("game_metrics")),
            },
            "units": units,
            "table_inventory": inventory,
            "tables": tables,
        }

    def snapshot(self, options):
        return copy.deepcopy(self.system_snapshot(options))

    def apply_monitoring(
        self, options, before, *, preserve_monitoring_table=False
    ):
        self.monitoring_active = True

    def install_live_base(self):
        self.bundle.options["router_live"] = True
        self.bundle.install_files()
        baseline = {
            "network": copy.deepcopy(self.network_state),
            "system": self.system_snapshot(self.bundle.options),
        }
        self.Lifecycle(self.bundle).save(
            {
                "format": 1,
                "phase": "active",
                "config": self.bundle.config,
                "options": copy.deepcopy(self.bundle.options),
                "baseline": copy.deepcopy(baseline),
                "applied": copy.deepcopy(baseline),
            }
        )

    def test_missing_live_base_is_rejected_before_dependencies(self):
        from hostlib.packages import Dependencies

        with patch.object(
            Dependencies, "install_monitoring", create=True
        ) as install_dependencies:
            with self.assertRaisesRegex(ValueError, "live router installation"):
                self.bundle.install_monitoring()
        install_dependencies.assert_not_called()

    def test_unowned_monitoring_destination_file_is_not_adopted(self):
        from hostlib.packages import Dependencies

        self.install_live_base()
        destination = self.bundle.path("/usr/local/bin/forcad")
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(b"unrelated executable")
        with patch.object(
            Dependencies, "install_monitoring", create=True
        ) as install_dependencies:
            with self.assertRaisesRegex(ValueError, "unowned monitoring file"):
                self.bundle.install_monitoring()
        install_dependencies.assert_not_called()
        self.assertEqual(destination.read_bytes(), b"unrelated executable")

    def test_unowned_monitoring_unit_is_not_adopted(self):
        from hostlib.packages import Dependencies

        self.install_live_base()
        original_snapshot = self.system.snapshot.side_effect

        def snapshot_with_unowned_unit(options):
            snapshot = original_snapshot(options)
            if options.get("game_metrics"):
                snapshot["units"]["game-metrics.service"]["load_state"] = "loaded"
            return snapshot

        self.system.snapshot.side_effect = snapshot_with_unowned_unit
        with patch.object(
            Dependencies, "install_monitoring", create=True
        ) as install_dependencies:
            with self.assertRaisesRegex(ValueError, "unowned monitoring unit"):
                self.bundle.install_monitoring()
        install_dependencies.assert_not_called()

    def test_monitoring_extends_live_base_without_reapplying_network(self):
        from hostlib.packages import Dependencies

        self.install_live_base()
        self.bundle.validate_monitoring = Mock()
        original_files = json.loads(self.bundle.manifest.read_text())["files"]
        with patch.object(
            Dependencies, "install_monitoring", create=True
        ) as install_dependencies:
            self.bundle.install_monitoring()
            install_dependencies.assert_called_once()

        manifest = json.loads(self.bundle.manifest.read_text())
        self.assertTrue(manifest["options"]["network_metrics"])
        self.assertTrue(manifest["options"]["game_metrics"])
        paths = {item["path"] for item in manifest["files"]}
        self.assertTrue({item["path"] for item in original_files} <= paths)
        self.assertIn("/etc/forcad/monitoring/network.json", paths)
        self.assertIn("/etc/forcad/monitoring/game-metrics.zeek", paths)

        journal = self.Lifecycle(self.bundle).load()
        self.assertTrue(journal["options"]["network_metrics"])
        self.assertTrue(journal["options"]["game_metrics"])
        self.assertEqual(journal["baseline"]["network"], self.network_state)
        self.network.apply.assert_not_called()
        self.system.prepare.assert_not_called()
        self.system.apply.assert_not_called()
        self.system.apply_monitoring.assert_called_once()

    def test_failed_monitoring_update_restores_base_manifest_and_runtime_journal(self):
        from hostlib.packages import Dependencies

        self.install_live_base()
        self.bundle.validate_monitoring = Mock()
        old_manifest = self.bundle.manifest.read_bytes()
        old_journal = self.Lifecycle(self.bundle).journal.read_bytes()
        self.system.apply_monitoring.side_effect = ValueError("sensor failed")
        with patch.object(Dependencies, "install_monitoring", create=True):
            with self.assertRaisesRegex(ValueError, "previous files and live state"):
                self.bundle.install_monitoring()

        self.assertEqual(self.bundle.manifest.read_bytes(), old_manifest)
        self.assertEqual(self.Lifecycle(self.bundle).journal.read_bytes(), old_journal)
        self.assertFalse(self.bundle.path("/etc/forcad/monitoring/game-metrics.zeek").exists())
        self.system.restore_monitoring.assert_called_once()
        self.network.apply.assert_not_called()
        self.system.stop_monitoring.assert_called_once()
        self.network.restore.assert_not_called()
        self.system.restore.assert_not_called()

    def test_stale_base_file_is_rejected_before_monitoring_dependencies(self):
        from hostlib.packages import Dependencies

        self.install_live_base()
        self.bundle.path("/etc/forcad/router/common.nft").write_text("# changed\n")
        old_journal = self.Lifecycle(self.bundle).journal.read_bytes()
        with patch.object(
            Dependencies, "install_monitoring", create=True
        ) as install_dependencies:
            with self.assertRaisesRegex(ValueError, "Installed file differs"):
                self.bundle.install_monitoring()
        install_dependencies.assert_not_called()
        self.assertEqual(self.Lifecycle(self.bundle).journal.read_bytes(), old_journal)

    def test_repeated_monitoring_install_keeps_single_file_record_per_path(self):
        from hostlib.packages import Dependencies

        self.install_live_base()
        self.bundle.validate_monitoring = Mock()
        with patch.object(Dependencies, "install_monitoring", create=True):
            self.bundle.install_monitoring()
            first = json.loads(self.bundle.manifest.read_text())
            self.bundle.install_monitoring()

        second = json.loads(self.bundle.manifest.read_text())
        paths = [item["path"] for item in second["files"]]
        self.assertEqual(len(paths), len(set(paths)))
        self.assertEqual(len(second["files"]), len(first["files"]))
        self.assertTrue(second["options"]["network_metrics"])
        self.assertTrue(second["options"]["game_metrics"])
        self.assertEqual(self.system.apply_monitoring.call_count, 2)
        self.assertTrue(
            self.system.apply_monitoring.call_args.kwargs["preserve_monitoring_table"]
        )

    def test_identical_active_monitoring_table_is_not_reapplied(self):
        from routerlib.system import (
            GAME_METRICS,
            MONITORING_TABLE,
            NETWORK_METRICS,
            System,
        )

        options = {"network_metrics": True, "game_metrics": True}
        self.monitoring_active = True
        before = self.system_snapshot(options)
        state = before["units"]["network-rules.service"]
        system = System(self.bundle)

        with (
            patch.object(system, "_require_live_root"),
            patch("routerlib.system.run") as run_command,
            patch.object(
                system, "_assert_firewall_clear", return_value={MONITORING_TABLE}
            ),
            patch.object(system, "_unit_state", return_value=state),
            patch.object(system, "_apply_network_metrics") as apply_network,
            patch.object(system, "_ensure_metrics_unit") as ensure_exporter,
            patch.object(system, "_ensure_permanent_enable") as enable_rules,
            patch.object(system, "verify_monitoring") as verify,
        ):
            system.apply_monitoring(
                options, before, preserve_monitoring_table=True
            )

        apply_network.assert_not_called()
        self.assertEqual(
            [call.args[0] for call in ensure_exporter.call_args_list],
            [NETWORK_METRICS, GAME_METRICS],
        )
        enable_rules.assert_called_once()
        verify.assert_called_once_with(options, before=before)
        run_command.assert_called_once_with("systemctl", "daemon-reload")


    def test_router_drift_ignores_only_runtime_relay_accounting_elements(self):
        from routerlib.system import System

        router_before = """table inet forcad_router {
    set relay_attempts {
        type ipv4_addr . ipv4_addr
        flags dynamic
        size 4096
        counter
        elements = { 10.183.101.2 . 10.183.102.2 counter packets 3 bytes 240 expires 59s }
    }
    set vulnboxes {
        type ipv4_addr
        elements = { 10.183.102.2 }
    }
    chain game_access {
        ip saddr 10.183.102.2 counter packets 8 bytes 512 accept
    }
}"""
        runtime_change = router_before.replace(
            "10.183.101.2 . 10.183.102.2 counter packets 3 bytes 240 expires 59s",
            "10.183.101.3 . 10.183.102.4 counter packets 7 bytes 560 expires 1s",
        ).replace("size 4096", "size 4096\t# count 1")
        def normalize(text, table="inet forcad_router"):
            return System._normalized_table_text(text, table)
        self.assertEqual(normalize(router_before), normalize(runtime_change))
        self.assertNotEqual(
            normalize(router_before, "inet foreign"),
            normalize(runtime_change, "inet foreign"),
        )
        self.assertNotEqual(
            normalize(router_before),
            normalize(router_before.replace("size 4096", "size 8192")),
        )
        static_member_change = router_before.replace(
            "elements = { 10.183.102.2 }",
            "elements = { 10.183.102.3 }",
        )
        self.assertNotEqual(
            normalize(router_before), normalize(static_member_change)
        )
        changed_acl = router_before.replace(
            "ip saddr 10.183.102.2 counter packets 8 bytes 512 accept",
            "ip saddr 10.183.102.3 counter packets 8 bytes 512 accept",
        )
        self.assertNotEqual(normalize(router_before), normalize(changed_acl))

        snapshot = self.system_snapshot({})
        snapshot["tables"]["inet forcad_router"] = router_before
        runtime_snapshot = copy.deepcopy(snapshot)
        runtime_snapshot["tables"]["inet forcad_router"] = runtime_change
        system = System(self.bundle)
        with patch.object(system, "_require_live_root"):
            system.check_restore(snapshot, snapshot, runtime_snapshot)
            acl_snapshot = copy.deepcopy(runtime_snapshot)
            acl_snapshot["tables"]["inet forcad_router"] = changed_acl
            with self.assertRaisesRegex(ValueError, "owned nftables tables"):
                system.check_restore(snapshot, snapshot, acl_snapshot)

    def test_monitoring_drift_ignores_source_set_expiry_changes(self):
        from routerlib.system import MONITORING_TABLE, System

        monitoring_before = """table inet forcad_monitoring {
    set sources_v4 {
        type ipv4_addr . ipv4_addr
        size 4096
        flags dynamic,timeout
        timeout 5m
        counter
        elements = { 10.183.200.2 . 10.183.102.2 counter packets 4 bytes 80 expires 4m59s }
    }
    set sources_v6 {
        type ipv6_addr . ipv6_addr
        size 4096
        flags dynamic,timeout
        timeout 5m
        counter
        elements = { 2001:db8::1 . 2001:db8::2 counter packets 3 bytes 60 expires 3m00s }
    }
    chain sources {
        ip daddr 10.183.102.2 update @sources_v4 { ip saddr . ip daddr counter }
        ip6 daddr 2001:db8::2 update @sources_v6 { ip6 saddr . ip6 daddr counter }
    }
}"""
        monitoring_after = monitoring_before.replace(
            "expires 4m59s", "expires 4m58s"
        ).replace("expires 3m00s", "expires 2m59s")

        options = {"network_metrics": True, "game_metrics": True}
        self.monitoring_active = True
        before = self.system_snapshot(options)
        before["tables"][MONITORING_TABLE] = monitoring_before
        after = copy.deepcopy(before)
        after["tables"][MONITORING_TABLE] = monitoring_after
        self.assertEqual(
            System._normalized_table_text(monitoring_before, MONITORING_TABLE),
            System._normalized_table_text(monitoring_after, MONITORING_TABLE),
        )

        system = System(self.bundle)
        with patch.object(system, "_require_live_root"):
            system.check_restore(before, before, after)

if __name__ == "__main__":
    unittest.main()

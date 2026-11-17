import copy
import json
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(ROOT / "router"))

from routerlib import system as router_system


class FakeBundle:
    def __init__(self, root=Path("/")):
        self.root = Path(root)
        path = self.path("/etc/forcad/monitoring/network.nft")
        self.changes = {
            path: (
                (b"table inet forcad_monitoring {\n    chain input { counter }\n}\n"),
                0o600,
            )
        }

    def path(self, absolute):
        return self.root / str(Path(absolute)).lstrip("/")


def unit(load="not-found", active="inactive", enabled=""):
    return {
        "load_state": load,
        "active_state": active,
        "unit_file_state": enabled,
    }


class Runtime:
    def __init__(self):
        self.units = {
            router_system.FIREWALL: unit(),
            router_system.NETWORKING: unit("loaded", "active", "enabled"),
            router_system.NFTABLES: unit("loaded", "inactive", "disabled"),
            router_system.NETWORK_RULES: unit(),
            router_system.NETWORK_METRICS: unit(),
            router_system.GAME_METRICS: unit(),
            "ufw.service": unit(),
            "firewalld.service": unit(),
        }
        self.tables = {}
        self.calls = []
        self.nft_scripts = []
        self.nft_events = []
        self.reload_states = {}

    def _inventory(self):
        entries = [{"metainfo": {"version": "1"}}]
        for key in sorted(self.tables):
            family, name = key.split(" ", 1)
            entries.append({"table": {"family": family, "name": name}})
        return json.dumps({"nftables": entries})

    def run(self, *args):
        self.calls.append(args)
        if args[:2] == ("systemctl", "show"):
            state = self.units[args[2]]
            return (
                f"LoadState={state['load_state']}\n"
                f"ActiveState={state['active_state']}\n"
                f"UnitFileState={state['unit_file_state']}\n"
            )
        if args[:2] == ("systemctl", "daemon-reload"):
            for name in (
                router_system.FIREWALL,
                router_system.NETWORK_RULES,
                router_system.NETWORK_METRICS,
                router_system.GAME_METRICS,
            ):
                state = self.units[name]
                if name in self.reload_states:
                    self.units[name] = copy.deepcopy(self.reload_states[name])
                    continue
                if state["load_state"] == "not-found":
                    state.update(
                        load_state="loaded",
                        active_state="inactive",
                        unit_file_state="disabled",
                    )
            return ""
        if args[0] == "systemctl" and "enable" in args:
            self.units[args[-1]]["unit_file_state"] = (
                "enabled-runtime" if "--runtime" in args else "enabled"
            )
            return ""
        if args[0] == "systemctl" and "disable" in args:
            self.units[args[-1]]["unit_file_state"] = "disabled"
            return ""
        if args[0] == "systemctl" and "mask" in args:
            self.units[args[-1]]["unit_file_state"] = (
                "masked-runtime" if "--runtime" in args else "masked"
            )
            return ""
        if args[0] == "systemctl" and "unmask" in args:
            self.units[args[-1]]["unit_file_state"] = "disabled"
            return ""
        if args[:2] in {
            ("systemctl", "start"),
            ("systemctl", "restart"),
            ("systemctl", "reload-or-restart"),
        }:
            name = args[2]
            self.units[name]["active_state"] = "active"
            if name == router_system.NETWORK_RULES:
                self.tables[router_system.MONITORING_TABLE] = (
                    "table inet forcad_monitoring {\n    chain input { counter }\n}\n"
                )
            return ""
        if args[:2] == ("systemctl", "stop"):
            self.units[args[2]]["active_state"] = "inactive"
            return ""
        if args[:3] == ("nft", "-j", "list"):
            return self._inventory()
        if args[:2] == ("nft", "list"):
            key = f"{args[3]} {args[4]}"
            return self.tables[key]
        if args[0] == "nft" and "-f" in args:
            script = Path(args[-1]).read_text()
            self.nft_scripts.append(("-c" in args, script))
            self.nft_events.append((len(self.calls) - 1, "-c" in args, script))
            if "-c" not in args:
                for key in list(self.tables):
                    family, name = key.split(" ", 1)
                    if f"delete table {family} {name}" in script:
                        del self.tables[key]
                for key in (
                    router_system.MONITORING_TABLE,
                    "inet forcad_router",
                    "ip forcad_nat",
                ):
                    declaration = f"table {key}"
                    lines = script.splitlines(keepends=True)
                    for index, line in enumerate(lines):
                        if line.lstrip().startswith(declaration + " "):
                            self.tables[key] = "".join(lines[index:])
                            break
            return ""
        if args[:1] == ("getent",):
            raise ValueError("entry not found")
        raise AssertionError(f"Unexpected command: {args}")


class RouterSystemTests(unittest.TestCase):
    def setUp(self):
        self.runtime = Runtime()
        self.bundle = FakeBundle()
        self.system = router_system.System(self.bundle)
        self.run_patch = patch.object(
            router_system, "run", side_effect=self.runtime.run
        )
        self.run_patch.start()
        self.addCleanup(self.run_patch.stop)
        stop_patch = patch.object(
            router_system,
            "stop_firewall",
            side_effect=lambda bundle, name: self.runtime.run(
                "systemctl", "stop", name
            ),
        )
        self.stop_firewall = stop_patch.start()
        self.addCleanup(stop_patch.stop)

    def snapshot(self, **options):
        return self.system.snapshot(options)

    def test_snapshot_captures_unit_states_and_owned_table_text(self):
        self.runtime.tables.update(
            {
                "inet forcad_router": "table inet forcad_router { }\n",
                "ip forcad_nat": "table ip forcad_nat { }\n",
            }
        )
        before = self.snapshot(network_metrics=True, game_metrics=True)

        self.assertEqual(
            before["units"][router_system.NETWORKING]["active_state"], "active"
        )
        self.assertEqual(
            before["units"][router_system.NFTABLES]["unit_file_state"], "disabled"
        )
        self.assertEqual(
            before["tables"],
            {
                "inet forcad_router": "table inet forcad_router { }\n",
                "inet forcad_monitoring": None,
                "ip forcad_nat": "table ip forcad_nat { }\n",
            },
        )
        self.assertIn(router_system.NETWORK_RULES, before["units"])
        self.assertIn(router_system.GAME_METRICS, before["units"])

    def test_snapshot_keeps_absent_selected_table_as_none(self):
        before = self.snapshot(network_metrics=True)

        self.assertEqual(
            set(before["tables"]),
            {
                "inet forcad_router",
                "ip forcad_nat",
                router_system.MONITORING_TABLE,
            },
        )
        self.assertIsNone(before["tables"][router_system.MONITORING_TABLE])

    def test_preflight_ignores_counter_drift_but_keeps_exact_snapshot_text(self):
        original = (
            "table inet forcad_router { chain input { counter packets 3 bytes 120 } }\n"
        )
        changed = (
            "table inet forcad_router { chain input { counter packets 8 bytes 640 } }\n"
        )
        self.runtime.tables["inet forcad_router"] = original
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)
        self.runtime.tables["inet forcad_router"] = changed

        self.system.preflight(before, options)

        self.assertEqual(before["tables"]["inet forcad_router"], original)

    def test_snapshot_rejects_injected_non_root_without_commands(self):
        system = router_system.System(FakeBundle(Path("/tmp/test-root")))

        with self.assertRaisesRegex(ValueError, "require the host root /"):
            system.snapshot({})

        self.assertEqual(self.runtime.calls, [])

    def test_preflight_rejects_active_ufw_before_mutation(self):
        self.runtime.units["ufw.service"] = unit("loaded", "active", "enabled")
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)
        call_count = len(self.runtime.calls)

        with self.assertRaisesRegex(ValueError, "other firewall manager"):
            self.system.preflight(before, options)

        later = self.runtime.calls[call_count:]
        self.assertFalse(any(call[:2] == ("systemctl", "stop") for call in later))
        self.assertFalse(any(call[:2] == ("systemctl", "enable") for call in later))

    def test_preflight_rejects_foreign_nft_table(self):
        self.runtime.tables["inet filter"] = "table inet filter { }\n"
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)

        with self.assertRaisesRegex(ValueError, "foreign tables"):
            self.system.preflight(before, options)

        self.assertFalse(
            any(
                call[:2] in {("systemctl", "stop"), ("systemctl", "enable")}
                for call in self.runtime.calls
            )
        )

    def test_preflight_rejects_monitoring_table_without_active_rules_unit(self):
        self.runtime.tables[router_system.MONITORING_TABLE] = (
            "table inet forcad_monitoring { }\n"
        )
        options = {"network_metrics": True, "game_metrics": False}
        before = self.system.snapshot(options)

        with self.assertRaisesRegex(ValueError, "state does not match"):
            self.system.preflight(before, options)

    def test_preflight_rejects_rules_that_flush_the_ruleset(self):
        path = self.bundle.path("/etc/forcad/monitoring/network.nft")
        self.bundle.changes[path] = (
            b"flush ruleset\ntable inet forcad_monitoring { }\n",
            0o600,
        )
        options = {"network_metrics": True, "game_metrics": False}
        before = self.system.snapshot(options)

        with self.assertRaisesRegex(ValueError, "must not flush"):
            self.system.preflight(before, options)

        self.assertFalse(
            any(
                call[:2] in {("systemctl", "stop"), ("systemctl", "enable")}
                for call in self.runtime.calls
            )
        )

    def test_game_metrics_preflight_requires_existing_sensor_account(self):
        options = {"network_metrics": False, "game_metrics": True}
        before = self.system.snapshot(options)
        call_count = len(self.runtime.calls)

        with self.assertRaisesRegex(ValueError, "valid system forcad-sensor"):
            self.system.preflight(before, options)

        later = self.runtime.calls[call_count:]
        self.assertFalse(any(call[:2] == ("systemctl", "enable") for call in later))

    def test_apply_activates_game_metrics_with_existing_sensor_account(self):
        options = {"network_metrics": False, "game_metrics": True}
        before = self.system.snapshot(options)
        original_run = self.runtime.run

        def account_run(*args):
            if args == ("getent", "passwd", "forcad-sensor"):
                return "forcad-sensor:x:123:123::/nonexistent:/usr/sbin/nologin\n"
            if args == ("getent", "group", "forcad-sensor"):
                return "forcad-sensor:x:123:\n"
            return original_run(*args)

        with patch.object(router_system, "run", side_effect=account_run):
            self.system.preflight(before, options)
            self.system.prepare(options, before)
            self.system.apply(options, before)

        self.assertEqual(
            self.runtime.units[router_system.GAME_METRICS]["active_state"], "active"
        )
        self.assertEqual(
            self.runtime.units[router_system.GAME_METRICS]["unit_file_state"], "enabled"
        )

    def test_apply_enables_firewall_and_metrics_without_restarting_networking(self):
        options = {"network_metrics": True, "game_metrics": False}
        before = self.system.snapshot(options)

        self.system.preflight(before, options)
        self.system.prepare(options, before)
        self.system.apply(options, before)

        self.assertEqual(
            self.runtime.units[router_system.FIREWALL]["active_state"], "active"
        )
        self.assertEqual(
            self.runtime.units[router_system.FIREWALL]["unit_file_state"], "enabled"
        )
        self.assertEqual(
            self.runtime.units[router_system.NETWORKING]["active_state"], "active"
        )
        self.assertEqual(
            self.runtime.units[router_system.NETWORKING]["unit_file_state"], "enabled"
        )
        self.assertEqual(
            self.runtime.units[router_system.NETWORK_METRICS]["active_state"], "active"
        )
        self.assertEqual(
            self.runtime.units[router_system.NETWORK_RULES]["active_state"], "active"
        )
        self.assertFalse(
            any(
                call[:2] in {("systemctl", "start"), ("systemctl", "restart")}
                and call[2] == router_system.NETWORKING
                for call in self.runtime.calls
            )
        )
        self.assertIn(
            ("systemctl", "reload-or-restart", router_system.FIREWALL),
            self.runtime.calls,
        )
        self.assertIn(
            ("systemctl", "start", router_system.NETWORK_RULES), self.runtime.calls
        )

    def test_reinstall_replaces_only_monitoring_table_without_restarting_rules(self):
        self.runtime.units[router_system.FIREWALL] = unit("loaded", "active", "enabled")
        self.runtime.units[router_system.NETWORK_RULES] = unit(
            "loaded", "active", "disabled"
        )
        self.runtime.units[router_system.NETWORK_METRICS] = unit(
            "loaded", "active", "enabled"
        )
        self.runtime.tables[router_system.MONITORING_TABLE] = (
            "table inet forcad_monitoring { chain input { counter } }\n"
        )
        options = {"network_metrics": True, "game_metrics": False}
        before = self.system.snapshot(options)

        self.system.preflight(before, options)
        self.system.prepare(options, before)
        self.system.apply(options, before)

        scripts = [script for check, script in self.runtime.nft_scripts if not check]
        self.assertTrue(
            any("delete table inet forcad_monitoring" in item for item in scripts)
        )
        self.assertFalse(any("flush ruleset" in item for item in scripts))
        self.assertFalse(
            any(
                call[:2] == ("systemctl", "restart")
                and call[2] == router_system.NETWORK_RULES
                for call in self.runtime.calls
            )
        )
        self.assertIn(
            ("systemctl", "enable", router_system.NETWORK_RULES), self.runtime.calls
        )
        self.assertEqual(
            self.runtime.units[router_system.NETWORK_RULES]["unit_file_state"],
            "enabled",
        )
        self.assertTrue(
            any(
                call[:2] == ("systemctl", "restart")
                and call[2] == router_system.NETWORK_METRICS
                for call in self.runtime.calls
            )
        )

    def test_restore_returns_stock_unit_and_scoped_tables_to_snapshot(self):
        self.runtime.units[router_system.NFTABLES] = unit("loaded", "active", "enabled")
        self.runtime.units[router_system.FIREWALL] = unit("not-found", "inactive", "")
        self.runtime.tables["inet forcad_router"] = (
            "table inet forcad_router { chain x { } }\n"
        )
        self.runtime.tables["ip forcad_nat"] = "table ip forcad_nat { chain x { } }\n"
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)

        self.runtime.units[router_system.NFTABLES] = unit(
            "loaded", "inactive", "disabled"
        )
        self.runtime.units[router_system.FIREWALL] = unit("loaded", "active", "enabled")
        self.runtime.tables["inet forcad_router"] = (
            "table inet forcad_router { chain changed { } }\n"
        )
        self.runtime.tables["ip forcad_nat"] = (
            "table ip forcad_nat { chain changed { } }\n"
        )

        self.system.restore(before)

        self.assertEqual(
            self.runtime.units[router_system.NFTABLES]["active_state"], "active"
        )
        self.assertEqual(
            self.runtime.units[router_system.NFTABLES]["unit_file_state"], "enabled"
        )
        self.assertEqual(
            self.runtime.units[router_system.FIREWALL]["active_state"], "inactive"
        )
        self.assertEqual(
            self.runtime.units[router_system.FIREWALL]["unit_file_state"], "disabled"
        )
        restore_scripts = [
            script for check, script in self.runtime.nft_scripts if not check
        ]
        self.assertTrue(
            any("delete table inet forcad_router" in item for item in restore_scripts)
        )
        self.assertTrue(
            any("delete table ip forcad_nat" in item for item in restore_scripts)
        )
        self.assertFalse(any("flush ruleset" in item for item in restore_scripts))
        self.assertFalse(
            any(
                call[:2] == ("systemctl", "enable")
                and call[2] == router_system.FIREWALL
                for call in self.runtime.calls
            )
        )

    def test_restore_rechecks_foreign_table_guard_before_starting_nftables(self):
        self.runtime.units[router_system.NFTABLES] = unit("loaded", "active", "enabled")
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)
        self.runtime.units[router_system.NFTABLES] = unit(
            "loaded", "inactive", "disabled"
        )
        original_run = self.runtime.run

        def inject_foreign_table(*args):
            result = original_run(*args)
            if args == ("systemctl", "enable", router_system.NFTABLES):
                self.runtime.tables["inet custom"] = "table inet custom { }\n"
            return result

        with (
            patch.object(router_system, "run", side_effect=inject_foreign_table),
            self.assertRaisesRegex(ValueError, "foreign tables"),
        ):
            self.system.restore(before)

        self.assertFalse(
            any(
                call[:2] == ("systemctl", "start") and call[2] == router_system.NFTABLES
                for call in self.runtime.calls
            )
        )

    def test_restore_reloads_existing_network_rules_without_double_loading_table(self):
        self.runtime.units[router_system.FIREWALL] = unit("loaded", "active", "enabled")
        self.runtime.units[router_system.NETWORK_RULES] = unit(
            "loaded", "active", "enabled"
        )
        self.runtime.units[router_system.NETWORK_METRICS] = unit(
            "loaded", "active", "enabled"
        )
        old_table = (
            "table inet forcad_monitoring { chain old { counter packets 1 bytes 2 } }\n"
        )
        self.runtime.tables[router_system.MONITORING_TABLE] = old_table
        options = {"network_metrics": True, "game_metrics": False}
        before = self.system.snapshot(options)
        self.runtime.tables[router_system.MONITORING_TABLE] = (
            "table inet forcad_monitoring { chain new { counter } }\n"
        )

        self.system.restore(before)

        self.assertEqual(self.runtime.tables[router_system.MONITORING_TABLE], old_table)
        nft_delete = next(
            index
            for index, _check, script in self.runtime.nft_events
            if "delete table inet forcad_monitoring" in script
        )
        start_rules = self.runtime.calls.index(
            ("systemctl", "start", router_system.NETWORK_RULES)
        )
        self.assertLess(nft_delete, start_rules)
        restored_after_start = [
            script
            for index, _check, script in self.runtime.nft_events
            if index > start_rules
        ]
        self.assertTrue(
            any(old_table.strip() in script for script in restored_after_start)
        )

    def test_preflight_preserves_masked_inactive_stock_nftables(self):
        self.runtime.units[router_system.NFTABLES] = unit(
            "loaded", "inactive", "masked"
        )
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)

        self.system.preflight(before, options)
        self.system.prepare(options, before)

        self.assertEqual(
            self.runtime.units[router_system.NFTABLES]["unit_file_state"], "masked"
        )
        self.assertFalse(
            any(
                call[:2] == ("systemctl", "disable")
                and call[2] == router_system.NFTABLES
                for call in self.runtime.calls
            )
        )

    def test_preflight_rejects_linked_or_alias_units_before_mutation(self):
        self.runtime.units[router_system.FIREWALL] = unit(
            "loaded", "inactive", "linked"
        )
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)
        call_count = len(self.runtime.calls)

        with self.assertRaisesRegex(ValueError, "linked or alias unit"):
            self.system.preflight(before, options)

        later = self.runtime.calls[call_count:]
        self.assertFalse(
            any(
                call[0] == "systemctl"
                and call[1] in {"enable", "disable", "start", "stop"}
                for call in later
            )
        )

    def test_verify_requires_permanent_enablement(self):
        self.runtime.units[router_system.FIREWALL] = unit(
            "loaded", "active", "enabled-runtime"
        )

        with self.assertRaisesRegex(ValueError, "not ready"):
            self.system.verify({})

    def test_install_converts_runtime_enablement_to_permanent(self):
        self.runtime.units[router_system.FIREWALL] = unit(
            "loaded", "active", "enabled-runtime"
        )
        self.runtime.units[router_system.NETWORKING] = unit(
            "loaded", "active", "enabled-runtime"
        )
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)

        self.system.preflight(before, options)
        self.system.prepare(options, before)
        self.system.apply(options, before)

        self.assertEqual(
            self.runtime.units[router_system.FIREWALL]["unit_file_state"], "enabled"
        )
        self.assertEqual(
            self.runtime.units[router_system.NETWORKING]["unit_file_state"],
            "enabled",
        )
        self.assertIn(
            ("systemctl", "--runtime", "disable", router_system.FIREWALL),
            self.runtime.calls,
        )
        self.assertIn(
            ("systemctl", "--runtime", "disable", router_system.NETWORKING),
            self.runtime.calls,
        )

    def test_restore_preserves_runtime_enabled_state(self):
        self.runtime.units[router_system.NFTABLES] = unit(
            "loaded", "inactive", "enabled-runtime"
        )
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)
        self.runtime.units[router_system.NFTABLES]["unit_file_state"] = "disabled"

        self.system.restore(before)

        self.assertEqual(
            self.runtime.units[router_system.NFTABLES]["unit_file_state"],
            "enabled-runtime",
        )
        self.assertIn(
            ("systemctl", "--runtime", "enable", router_system.NFTABLES),
            self.runtime.calls,
        )

    def test_restore_preserves_runtime_mask(self):
        self.runtime.units[router_system.NFTABLES] = unit(
            "loaded", "inactive", "masked-runtime"
        )
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)
        self.runtime.units[router_system.NFTABLES]["unit_file_state"] = "disabled"

        self.system.restore(before)

        self.assertEqual(
            self.runtime.units[router_system.NFTABLES]["unit_file_state"],
            "masked-runtime",
        )
        self.assertIn(
            ("systemctl", "--runtime", "mask", router_system.NFTABLES),
            self.runtime.calls,
        )

    def test_restore_preserves_permanent_mask(self):
        self.runtime.units[router_system.NFTABLES] = unit(
            "loaded", "inactive", "masked"
        )
        options = {"network_metrics": False, "game_metrics": False}
        before = self.system.snapshot(options)
        self.runtime.units[router_system.NFTABLES]["unit_file_state"] = "disabled"

        self.system.restore(before)

        self.assertEqual(
            self.runtime.units[router_system.NFTABLES]["unit_file_state"], "masked"
        )
        self.assertIn(("systemctl", "mask", router_system.NFTABLES), self.runtime.calls)

    def test_check_restore_rejects_owned_runtime_drift_before_mutation(self):
        self.runtime.tables["inet forcad_router"] = (
            "table inet forcad_router { chain input { counter packets 2 bytes 90 } }\n"
        )
        options = {"network_metrics": False, "game_metrics": False}
        baseline = self.system.snapshot(options)
        applied = copy.deepcopy(baseline)
        self.runtime.tables["inet forcad_router"] = (
            "table inet forcad_router { chain input { counter packets 7 bytes 420 } }\n"
        )
        current = self.system.snapshot(options)

        self.system.check_restore(baseline, applied, current)
        self.runtime.units[router_system.FIREWALL]["unit_file_state"] = "enabled"
        current = self.system.snapshot(options)
        call_count = len(self.runtime.calls)

        with self.assertRaisesRegex(ValueError, "differs from saved state"):
            self.system.check_restore(baseline, applied, current)

        self.assertFalse(
            any(
                call[0] == "systemctl"
                and call[1] in {"enable", "disable", "start", "stop"}
                for call in self.runtime.calls[call_count:]
            )
        )

    def test_check_restore_force_allows_owned_drift_but_not_foreign_tables(self):
        options = {"network_metrics": False, "game_metrics": False}
        baseline = self.system.snapshot(options)
        applied = copy.deepcopy(baseline)
        self.runtime.units[router_system.FIREWALL]["unit_file_state"] = "enabled"
        current = self.system.snapshot(options)

        self.system.check_restore(baseline, applied, current, force=True)

        self.runtime.tables["inet custom"] = "table inet custom { }\n"
        current = self.system.snapshot(options)
        with self.assertRaisesRegex(ValueError, "foreign tables"):
            self.system.check_restore(baseline, applied, current, force=True)

    def test_changed_nftables_service_can_return_to_saved_state(self):
        options = {"network_metrics": False, "game_metrics": False}
        baseline = self.system.snapshot(options)
        applied = copy.deepcopy(baseline)
        self.runtime.units[router_system.NFTABLES] = unit("loaded", "active", "enabled")
        current = self.system.snapshot(options)
        self.system.check_restore(baseline, applied, current, allow_unit_changes=True)
        self.system.restore(baseline)
        self.assertEqual(
            self.runtime.units[router_system.NFTABLES],
            baseline["units"][router_system.NFTABLES],
        )

    def test_restore_starts_preexisting_networking_if_it_was_stopped(self):
        options = {"network_metrics": False, "game_metrics": False}
        baseline = self.system.snapshot(options)
        self.runtime.units[router_system.NETWORKING]["active_state"] = "inactive"
        self.system.restore(baseline)
        self.assertIn(
            ("systemctl", "start", router_system.NETWORKING), self.runtime.calls
        )

    def test_game_switch_does_not_waive_monitoring_table_drift(self):
        options = {"network_metrics": True, "game_metrics": False}
        self.runtime.tables["inet forcad_router"] = (
            "table inet forcad_router { chain phase { counter } }\n"
        )
        self.runtime.tables[router_system.MONITORING_TABLE] = (
            "table inet forcad_monitoring { chain input { counter } }\n"
        )
        baseline = self.system.snapshot(options)
        applied = copy.deepcopy(baseline)
        self.runtime.tables["inet forcad_router"] = (
            "table inet forcad_router { chain phase { counter accept } }\n"
        )
        current = self.system.snapshot(options)

        self.system.check_restore(baseline, applied, current, allow_game_switch=True)

        self.runtime.tables[router_system.MONITORING_TABLE] = (
            "table inet forcad_monitoring { chain input { counter drop } }\n"
        )
        current = self.system.snapshot(options)
        with self.assertRaisesRegex(ValueError, "owned nftables tables"):
            self.system.check_restore(
                baseline, applied, current, allow_game_switch=True
            )

    def test_stop_only_stops_selected_forcad_units_in_dependency_order(self):
        for name in (
            router_system.FIREWALL,
            router_system.NETWORK_RULES,
            router_system.NETWORK_METRICS,
            router_system.GAME_METRICS,
        ):
            self.runtime.units[name] = unit("loaded", "active", "enabled")

        self.system.stop({"network_metrics": True, "game_metrics": True})

        stops = [
            call[2] for call in self.runtime.calls if call[:2] == ("systemctl", "stop")
        ]
        disables = [
            call[2]
            for call in self.runtime.calls
            if call[:2] == ("systemctl", "disable")
        ]
        self.assertEqual(
            stops,
            [
                router_system.NETWORK_METRICS,
                router_system.GAME_METRICS,
                router_system.NETWORK_RULES,
                router_system.FIREWALL,
            ],
        )
        self.assertEqual(disables, stops)

    def test_restore_reloads_deleted_unit_before_reading_file_state(self):
        before = self.snapshot()
        self.runtime.units[router_system.FIREWALL] = unit("loaded", "inactive", "bad")
        self.runtime.reload_states[router_system.FIREWALL] = unit()
        self.runtime.calls.clear()

        self.system.restore(before)

        self.assertEqual(self.runtime.calls[0], ("systemctl", "daemon-reload"))
        self.assertEqual(
            self.runtime.units[router_system.NETWORKING]["active_state"], "active"
        )
        self.assertEqual(
            self.runtime.units[router_system.FIREWALL]["load_state"], "not-found"
        )

    def test_stop_reloads_restored_unit_before_recovery(self):
        self.runtime.units[router_system.FIREWALL] = unit("loaded", "inactive", "bad")
        self.runtime.reload_states[router_system.FIREWALL] = unit(
            "loaded", "active", "disabled"
        )

        self.system.stop({})

        self.stop_firewall.assert_called_once_with(self.bundle, router_system.FIREWALL)
        self.assertEqual(
            self.runtime.units[router_system.NETWORKING]["active_state"], "active"
        )

    def test_missing_inactive_unit_has_no_install_state_to_restore(self):
        self.runtime.units[router_system.FIREWALL] = unit(
            "not-found", "inactive", "bad"
        )
        state = self.system._unit_state(router_system.FIREWALL)
        self.assertEqual(state["unit_file_state"], "not-found")

    def test_reload_does_not_hide_a_broken_existing_unit(self):
        before = self.snapshot()
        self.runtime.units[router_system.FIREWALL] = unit("loaded", "inactive", "bad")
        with self.assertRaisesRegex(ValueError, "unit-file state 'bad'"):
            self.system.restore(before)
        self.stop_firewall.assert_not_called()


if __name__ == "__main__":
    unittest.main()

"""Router runtime changes, scoped to ForcAD's own units and nftables tables."""

import json
import os
import re
import tempfile
from pathlib import Path

from hostlib.common import run
from hostlib.services import stop_firewall

FIREWALL = "forcad-router-firewall.service"
NETWORKING = "networking.service"
NFTABLES = "nftables.service"
NETWORK_RULES = "network-rules.service"
NETWORK_METRICS = "network-metrics.service"
GAME_METRICS = "game-metrics.service"
FIREWALL_MANAGERS = ("ufw.service", "firewalld.service")

BASE_TABLES = {"inet forcad_router", "ip forcad_nat"}
MONITORING_TABLE = "inet forcad_monitoring"
ENABLED_STATES = {
    "enabled",
    "enabled-runtime",
}
SUPPORTED_UNIT_FILE_STATES = ENABLED_STATES | {
    "",
    "disabled",
    "not-found",
    "static",
    "masked",
    "masked-runtime",
}
ACTIVE_STATES = {"active", "activating", "reloading", "deactivating"}
MUTATES_NFT = {FIREWALL, NETWORK_RULES, NFTABLES}


class System:
    """Apply and restore router runtime state for an injected host bundle."""

    def __init__(self, bundle):
        self.bundle = bundle
        self.root = Path(bundle.root)
        self._prepared = False

    @staticmethod
    def _options(options):
        return {
            "network_metrics": bool(options.get("network_metrics", False)),
            "game_metrics": bool(options.get("game_metrics", False)),
        }

    def _require_live_root(self):
        if self.root != Path("/"):
            raise ValueError("Router runtime actions require the host root /")

    def _units_for(self, options):
        names = [FIREWALL, NETWORKING, NFTABLES, *FIREWALL_MANAGERS]
        if options["network_metrics"]:
            names.extend((NETWORK_RULES, NETWORK_METRICS))
        if options["game_metrics"]:
            names.append(GAME_METRICS)
        return tuple(dict.fromkeys(names))

    @staticmethod
    def _validate_unit_states(units):
        for unit, state in units.items():
            if not isinstance(state, dict):
                raise TypeError(f"Invalid systemd state for {unit}")
            file_state = state.get("unit_file_state")
            if not isinstance(file_state, str):
                raise TypeError(f"Invalid unit-file state for {unit}")
            if file_state in {"linked", "linked-runtime", "alias"}:
                raise ValueError(
                    f"Refusing router runtime changes for linked or alias unit: {unit}"
                )
            if file_state not in SUPPORTED_UNIT_FILE_STATES:
                raise ValueError(
                    f"Cannot safely restore unit-file state {file_state!r} for {unit}"
                )
            if not isinstance(state.get("load_state"), str) or not isinstance(
                state.get("active_state"), str
            ):
                raise TypeError(f"Invalid systemd state for {unit}")

    @staticmethod
    def _unit_state(unit):
        output = run(
            "systemctl",
            "show",
            unit,
            "--property=LoadState,ActiveState,UnitFileState",
        )
        properties = {}
        for line in output.splitlines():
            key, separator, value = line.partition("=")
            if separator:
                properties[key] = value
        required = ("LoadState", "ActiveState", "UnitFileState")
        if any(key not in properties for key in required):
            raise ValueError(f"Could not read systemd state for {unit}")
        if (
            properties["LoadState"] == "not-found"
            and properties["ActiveState"] == "inactive"
            and properties["UnitFileState"] == "bad"
        ):
            properties["UnitFileState"] = "not-found"
        return {
            "load_state": properties["LoadState"],
            "active_state": properties["ActiveState"],
            "unit_file_state": properties["UnitFileState"],
        }

    @staticmethod
    def _table_key(family, name):
        if not re.fullmatch(r"[a-z0-9_]+", family) or not re.fullmatch(
            r"[a-zA-Z0-9_.-]+", name
        ):
            raise ValueError("Invalid nftables table name")
        return f"{family} {name}"

    @classmethod
    def _table_inventory(cls):
        try:
            result = json.loads(run("nft", "-j", "list", "tables"))
        except json.JSONDecodeError as error:
            raise ValueError("Could not parse nftables table inventory") from error
        entries = result.get("nftables") if isinstance(result, dict) else None
        if not isinstance(entries, list):
            raise TypeError("Could not read nftables table inventory")
        tables = set()
        for entry in entries:
            item = entry.get("table") if isinstance(entry, dict) else None
            if not isinstance(item, dict):
                continue
            tables.add(cls._table_key(item.get("family", ""), item.get("name", "")))
        return tables

    @staticmethod
    def _allowed_tables(options):
        tables = set(BASE_TABLES)
        if options["network_metrics"]:
            tables.add(MONITORING_TABLE)
        return tables

    @classmethod
    def _check_table_inventory(cls, tables, options):
        foreign = set(tables) - cls._allowed_tables(options)
        if foreign:
            raise ValueError(
                "Refusing to change nftables while foreign tables exist: "
                + ", ".join(sorted(foreign))
            )

    @staticmethod
    def _capture_tables(inventory, options):
        present = set(inventory)
        captured = {}
        for key in sorted(System._allowed_tables(options)):
            if key not in present:
                captured[key] = None
                continue
            family, name = key.split(" ", 1)
            captured[key] = run("nft", "list", "table", family, name)
        return captured

    @staticmethod
    def _normalized_table_text(text):
        if text is None:
            return None
        return re.sub(
            r"\bcounter\s+packets\s+\d+\s+bytes\s+\d+",
            "counter packets 0 bytes 0",
            text,
            flags=re.IGNORECASE,
        )

    def snapshot(self, options):
        """Read unit states and exact text for currently loaded owned tables."""
        self._require_live_root()
        selected = self._options(options)
        units = {name: self._unit_state(name) for name in self._units_for(selected)}
        inventory = sorted(self._table_inventory())
        return {
            "role": "router",
            "options": selected,
            "units": units,
            "table_inventory": inventory,
            "tables": self._capture_tables(inventory, selected),
        }

    def _assert_firewall_clear(self, options, inventory=None):
        for manager in FIREWALL_MANAGERS:
            state = self._unit_state(manager)
            if state["active_state"] in ACTIVE_STATES:
                raise ValueError(f"Stop the other firewall manager first: {manager}")
        if inventory is None:
            inventory = self._table_inventory()
        self._check_table_inventory(inventory, options)
        return inventory

    @staticmethod
    def _require_not_masked(unit, state):
        if state["unit_file_state"] in {"masked", "masked-runtime"}:
            raise ValueError(f"Unmask the required unit before applying: {unit}")

    def _network_rules_bytes(self):
        destination = self.bundle.path("/etc/forcad/monitoring/network.nft")
        staged = getattr(self.bundle, "changes", {}).get(destination)
        if staged is not None:
            return staged[0]
        if destination.is_file():
            return destination.read_bytes()
        raise ValueError("Network metrics rules are missing from the router bundle")

    @staticmethod
    def _validate_monitoring_rules(data):
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError as error:
            raise ValueError("Network metrics rules are not UTF-8 text") from error
        if re.search(r"(?im)^\s*(?:flush\s+ruleset|include\b)", text):
            raise ValueError("Network metrics rules must not flush or include rulesets")
        table_refs = re.findall(
            r"(?im)^\s*(?:table\s+)?(?:add\s+|delete\s+|destroy\s+|flush\s+)?"
            r"(?:table|chain|rule|set|map|element)\s+([a-z0-9_]+)\s+"
            r"([a-zA-Z0-9_.-]+)",
            text,
        )
        if any(f"{family} {name}" != MONITORING_TABLE for family, name in table_refs):
            raise ValueError("Network metrics rules must touch only forcad_monitoring")
        if not table_refs:
            raise ValueError("Network metrics rules do not define forcad_monitoring")
        return text

    @staticmethod
    def _run_nft_script(text, check=False):
        descriptor, filename = tempfile.mkstemp(prefix="forcad-", suffix=".nft")
        try:
            os.fchmod(descriptor, 0o600)
            with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
                stream.write(text)
            command = (
                ("nft", "-c", "-f", filename) if check else ("nft", "-f", filename)
            )
            return run(*command)
        finally:
            Path(filename).unlink(missing_ok=True)

    def _monitoring_script(self, data, inventory):
        text = self._validate_monitoring_rules(data)
        script = ""
        if MONITORING_TABLE in inventory:
            script += f"delete table {MONITORING_TABLE}\n"
        return script + text

    @staticmethod
    def _check_snapshot_shape(snapshot):
        if snapshot.get("role") != "router":
            raise ValueError("Runtime snapshot belongs to another host role")
        if not isinstance(snapshot.get("units"), dict) or not isinstance(
            snapshot.get("tables"), dict
        ):
            raise TypeError("Invalid router runtime snapshot")

    def preflight(self, snapshot, options):
        """Reject local ownership collisions before installation writes files."""
        self._require_live_root()
        self._check_snapshot_shape(snapshot)
        selected = self._options(options)
        if selected != snapshot.get("options"):
            raise ValueError("Runtime options differ from the captured snapshot")

        current_units = {
            name: self._unit_state(name) for name in self._units_for(selected)
        }
        self._validate_unit_states(snapshot["units"])
        self._validate_unit_states(current_units)
        current_inventory = sorted(self._table_inventory())
        current_tables = self._capture_tables(current_inventory, selected)
        if current_units != snapshot["units"] or current_inventory != snapshot.get(
            "table_inventory"
        ):
            raise ValueError(
                "Host runtime state changed after snapshot; capture it again"
            )
        if {
            key: self._normalized_table_text(text)
            for key, text in current_tables.items()
        } != {
            key: self._normalized_table_text(text)
            for key, text in snapshot["tables"].items()
        }:
            raise ValueError("An owned nftables table changed after snapshot")

        self._assert_firewall_clear(selected, current_inventory)
        transitioning = [
            name
            for name, state in current_units.items()
            if state["active_state"] in {"activating", "deactivating", "reloading"}
        ]
        if transitioning:
            raise ValueError(
                "Wait for these services to finish changing before router setup: "
                + ", ".join(transitioning)
            )
        networking = current_units[NETWORKING]
        if networking["load_state"] != "loaded":
            raise ValueError("networking.service must be installed before router setup")
        if networking["active_state"] != "active":
            raise ValueError(
                "networking.service must already be active; setup does not restart it"
            )
        self._require_not_masked(NETWORKING, networking)

        if selected["network_metrics"]:
            rules_state = current_units[NETWORK_RULES]
            metrics_state = current_units[NETWORK_METRICS]
            table_exists = MONITORING_TABLE in current_inventory
            if (rules_state["active_state"] == "active") != table_exists:
                raise ValueError(
                    "network-rules.service state does not match forcad_monitoring; resolve it manually"
                )
            if metrics_state["active_state"] == "active" and not table_exists:
                raise ValueError(
                    "network-metrics.service is active without forcad_monitoring"
                )
            self._require_not_masked(NETWORK_RULES, rules_state)
            self._require_not_masked(NETWORK_METRICS, metrics_state)
            text = self._monitoring_script(
                self._network_rules_bytes(), set(current_inventory)
            )
            self._run_nft_script(text, check=True)

        if selected["game_metrics"]:
            self._require_not_masked(GAME_METRICS, current_units[GAME_METRICS])
            self._check_sensor_account()

        firewall = current_units[FIREWALL]
        self._require_not_masked(FIREWALL, firewall)

    @staticmethod
    def _check_sensor_account():
        try:
            passwd = run("getent", "passwd", "forcad-sensor").strip().split(":")
            group = run("getent", "group", "forcad-sensor").strip().split(":")
            uid = int(passwd[2])
            gid = int(passwd[3])
            group_gid = int(group[2])
        except (ValueError, IndexError) as error:
            raise ValueError(
                "Create a valid system forcad-sensor user and group before applying game metrics"
            ) from error
        if (
            len(passwd) != 7
            or len(group) != 4
            or passwd[0] != "forcad-sensor"
            or group[0] != "forcad-sensor"
            or not 0 < uid < 1000
            or not 0 < gid < 1000
            or gid != group_gid
            or Path(passwd[6]).name != "nologin"
        ):
            raise ValueError(
                "forcad-sensor must be a system account with a nologin shell and matching primary group"
            )

    def _guarded_command(self, action, unit, options):
        if unit in MUTATES_NFT and action in {
            "start",
            "stop",
            "restart",
            "reload-or-restart",
        }:
            self._assert_firewall_clear(options)
        if action == "stop" and unit == FIREWALL:
            return stop_firewall(self.bundle, unit)
        return run("systemctl", action, unit)

    def prepare(self, options, before):
        """Load installed units, retire stock nftables, and activate the firewall."""
        self._require_live_root()
        selected = self._options(options)
        self._check_snapshot_shape(before)
        if selected != before.get("options"):
            raise ValueError("Runtime options differ from the captured snapshot")
        if self._prepared:
            return
        self._validate_unit_states(before["units"])
        self._validate_unit_states(
            {name: self._unit_state(name) for name in self._units_for(selected)}
        )
        self._assert_firewall_clear(selected)
        run("systemctl", "daemon-reload")

        stock = self._unit_state(NFTABLES)
        if stock["active_state"] in ACTIVE_STATES:
            self._guarded_command("stop", NFTABLES, selected)
        if stock["unit_file_state"] in ENABLED_STATES:
            self._assert_firewall_clear(selected)
            run("systemctl", "disable", NFTABLES)

        networking = self._unit_state(NETWORKING)
        self._require_not_masked(NETWORKING, networking)
        self._ensure_permanent_enable(NETWORKING, networking)

        firewall = self._unit_state(FIREWALL)
        if firewall["load_state"] != "loaded":
            raise ValueError("forcad-router-firewall.service did not load")
        self._require_not_masked(FIREWALL, firewall)
        self._ensure_permanent_enable(FIREWALL, firewall)
        self._guarded_command("reload-or-restart", FIREWALL, selected)
        self._prepared = True

    def _replace_monitoring_table(self, options):
        inventory = self._assert_firewall_clear(options)
        data = self._network_rules_bytes()
        script = self._monitoring_script(data, inventory)
        self._run_nft_script(script, check=True)
        self._run_nft_script(script)

    def _ensure_metrics_unit(self, unit, options):
        state = self._unit_state(unit)
        self._require_not_masked(unit, state)
        self._ensure_permanent_enable(unit, state)
        if state["active_state"] == "active":
            run("systemctl", "restart", unit)
        else:
            run("systemctl", "start", unit)

    def _apply_network_metrics(self, options):
        inventory = self._assert_firewall_clear(options)
        rules = self._unit_state(NETWORK_RULES)
        self._require_not_masked(NETWORK_RULES, rules)
        self._ensure_permanent_enable(NETWORK_RULES, rules)
        if rules["active_state"] == "active":
            self._replace_monitoring_table(options)
        elif MONITORING_TABLE in inventory:
            raise ValueError(
                "forcad_monitoring exists while network-rules.service is inactive"
            )
        else:
            self._guarded_command("start", NETWORK_RULES, options)
            if MONITORING_TABLE not in self._assert_firewall_clear(options):
                raise ValueError(
                    "network-rules.service did not create forcad_monitoring"
                )
        self._ensure_metrics_unit(NETWORK_METRICS, options)

    def apply(self, options, before):
        """Enable selected router services without restarting networking."""
        self._require_live_root()
        selected = self._options(options)
        if not self._prepared:
            self.prepare(selected, before)
        if selected["network_metrics"]:
            self._apply_network_metrics(selected)
        if selected["game_metrics"]:
            self._check_sensor_account()
            self._ensure_metrics_unit(GAME_METRICS, selected)
        self.verify(selected)

    def _managed_units(self, options):
        units = [FIREWALL]
        if options["network_metrics"]:
            units.extend((NETWORK_METRICS, NETWORK_RULES))
        if options["game_metrics"]:
            units.append(GAME_METRICS)
        return tuple(units)

    def stop(self, options):
        """Stop selected ForcAD units before their staged files are removed."""
        self._require_live_root()
        # Recovery can reach this method after unit files were removed or restored.
        run("systemctl", "daemon-reload")
        selected = self._options(options)
        order = []
        if selected["network_metrics"]:
            order.append(NETWORK_METRICS)
        if selected["game_metrics"]:
            order.append(GAME_METRICS)
        if selected["network_metrics"]:
            order.append(NETWORK_RULES)
        order.append(FIREWALL)
        states = {unit: self._unit_state(unit) for unit in order}
        self._validate_unit_states(states)
        for unit in order:
            state = states[unit]
            if state["active_state"] in ACTIVE_STATES:
                self._guarded_command("stop", unit, selected)
            if (
                state["load_state"] != "not-found"
                and state["unit_file_state"] in ENABLED_STATES
            ):
                run("systemctl", "disable", unit)

    @staticmethod
    def _ensure_permanent_enable(unit, state):
        file_state = state["unit_file_state"]
        if file_state == "enabled":
            return
        if file_state == "enabled-runtime":
            run("systemctl", "--runtime", "disable", unit)
        elif file_state in {"masked", "masked-runtime"}:
            raise ValueError(f"Unmask the required unit before applying: {unit}")
        run("systemctl", "enable", unit)

    @staticmethod
    def _set_unit_file_state(unit, wanted, current):
        if wanted == current:
            return
        if current == "masked-runtime":
            run("systemctl", "--runtime", "unmask", unit)
        if current == "masked":
            run("systemctl", "unmask", unit)

        if current == "enabled-runtime":
            run("systemctl", "--runtime", "disable", unit)
        elif current == "enabled":
            run("systemctl", "disable", unit)

        if wanted == "enabled":
            run("systemctl", "enable", unit)
        elif wanted == "enabled-runtime":
            run("systemctl", "--runtime", "enable", unit)
        elif wanted == "masked":
            run("systemctl", "mask", unit)
        elif wanted == "masked-runtime":
            run("systemctl", "--runtime", "mask", unit)

    def _set_restored_unit_file_state(self, unit, wanted, current, options):
        if unit == NFTABLES and wanted != current:
            self._assert_firewall_clear(options)
        self._set_unit_file_state(unit, wanted, current)

    def _restore_unit_states(self, snapshot, options):
        units = snapshot["units"]
        current = {name: self._unit_state(name) for name in units}
        self._validate_unit_states(units)
        self._validate_unit_states(current)
        order = [NFTABLES, FIREWALL, NETWORK_RULES, NETWORK_METRICS, GAME_METRICS]
        order += [name for name in units if name not in order]

        for unit in order:
            if unit not in units:
                continue
            baseline = units[unit]
            state = current[unit]
            if baseline["load_state"] == "not-found":
                if state["load_state"] == "not-found":
                    continue
                if state["active_state"] in ACTIVE_STATES:
                    self._guarded_command("stop", unit, options)
                if state["unit_file_state"] in ENABLED_STATES:
                    self._set_restored_unit_file_state(
                        unit, "disabled", state["unit_file_state"], options
                    )
                continue
            if state["load_state"] == "not-found":
                raise ValueError(f"Cannot restore missing pre-existing unit: {unit}")
            self._set_restored_unit_file_state(
                unit,
                baseline["unit_file_state"],
                state["unit_file_state"],
                options,
            )

        for unit in order:
            if unit not in units or units[unit]["load_state"] == "not-found":
                continue
            baseline = units[unit]
            current_state = self._unit_state(unit)
            wanted_active = baseline["active_state"] == "active"
            is_active = current_state["active_state"] in ACTIVE_STATES
            if unit == NETWORKING:
                if wanted_active != is_active:
                    self._guarded_command(
                        "start" if wanted_active else "stop", unit, options
                    )
                continue
            if wanted_active and not is_active:
                if unit == NETWORK_RULES:
                    self._clear_monitoring_table(options)
                self._guarded_command("start", unit, options)
            elif not wanted_active and is_active:
                self._guarded_command("stop", unit, options)

    def _clear_monitoring_table(self, options):
        inventory = self._assert_firewall_clear(options)
        if MONITORING_TABLE not in inventory:
            return
        script = f"delete table {MONITORING_TABLE}\n"
        self._run_nft_script(script, check=True)
        self._run_nft_script(script)

    def _restore_tables(self, snapshot, options):
        inventory = self._assert_firewall_clear(options)
        script = "".join(
            f"delete table {key}\n"
            for key in sorted(set(inventory) & self._allowed_tables(options))
        )
        for key, text in sorted(snapshot["tables"].items()):
            if key not in self._allowed_tables(options):
                raise ValueError(
                    f"Runtime snapshot contains an unowned nftables table: {key}"
                )
            if text is None:
                continue
            script += text.rstrip() + "\n"
        if script:
            self._run_nft_script(script, check=True)
            self._run_nft_script(script)

    def restore(self, snapshot):
        """Restore pre-transaction unit states and only captured owned tables."""
        self._require_live_root()
        self._check_snapshot_shape(snapshot)
        self._validate_unit_states(snapshot["units"])
        options = self._options(snapshot.get("options", {}))
        run("systemctl", "daemon-reload")
        self._validate_unit_states(
            {name: self._unit_state(name) for name in snapshot["units"]}
        )
        self._assert_firewall_clear(options)
        self.stop(options)
        run("systemctl", "daemon-reload")
        self._restore_unit_states(snapshot, options)
        self._restore_tables(snapshot, options)
        self._prepared = False

    def verify(self, options):
        """Require the selected units to be loaded, active, and boot-enabled."""
        self._require_live_root()
        selected = self._options(options)
        required = [FIREWALL, NETWORKING]
        if selected["network_metrics"]:
            required.extend((NETWORK_RULES, NETWORK_METRICS))
        if selected["game_metrics"]:
            required.append(GAME_METRICS)
        failures = []
        for unit in required:
            state = self._unit_state(unit)
            if (
                state["load_state"] != "loaded"
                or state["active_state"] != "active"
                or state["unit_file_state"] != "enabled"
            ):
                failures.append(
                    f"{unit} (load={state['load_state']}, active={state['active_state']}, enabled={state['unit_file_state']})"
                )
        if failures:
            raise ValueError(
                "Router runtime services are not ready: " + ", ".join(failures)
            )

    def check_restore(
        self,
        baseline,
        applied,
        current,
        force=False,
        *,
        allow_game_switch=False,
        allow_unit_changes=False,
    ):
        """Reject unplanned runtime drift before uninstall starts mutating state."""
        self._require_live_root()
        for snapshot in (baseline, applied, current):
            self._check_snapshot_shape(snapshot)
            self._validate_unit_states(snapshot["units"])

        options = self._options(applied.get("options", {}))
        if self._options(current.get("options", {})) != options:
            raise ValueError("Current runtime snapshot has different router options")
        inventory = current.get("table_inventory")
        applied_inventory = applied.get("table_inventory")
        if not isinstance(inventory, list) or not isinstance(applied_inventory, list):
            raise TypeError("Runtime snapshot has no nftables inventory")
        # Force applies only to owned-state drift. Unknown tables remain a hard stop.
        self._check_table_inventory(inventory, options)

        drift = []
        for unit, expected in applied["units"].items():
            if current["units"].get(unit) != expected and not allow_unit_changes:
                drift.append(f"systemd unit {unit}")

        inventory_changed = set(applied_inventory) != set(inventory)
        expected_tables = applied["tables"]
        current_tables = current["tables"]
        tables_changed = set(expected_tables) != set(current_tables)
        for key in set(expected_tables) & set(current_tables):
            if allow_game_switch and key in BASE_TABLES:
                continue
            if self._normalized_table_text(expected_tables[key]) != (
                self._normalized_table_text(current_tables[key])
            ):
                tables_changed = True
                break
        if inventory_changed or tables_changed:
            drift.append("owned nftables tables")
        if drift and not force:
            raise ValueError(
                "Router runtime differs from saved state: " + ", ".join(drift)
            )

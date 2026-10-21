"""Apply and restore the VPN host's network and service state."""

import fcntl
import hashlib
import json
import os
import re
import tempfile
from contextlib import contextmanager
from copy import deepcopy
from pathlib import Path

from hostlib.common import regular, run
from hostlib.files import atomic_write, rollback_files
from hostlib.services import stop_firewall

FIREWALL = "forcad-vpn-firewall.service"
WG_UNITS = ("wg-quick@wg-attack.service", "wg-quick@wg-admin.service")
NETWORKING = "networking.service"
NFTABLES = "nftables.service"
FIREWALL_MANAGERS = ("ufw.service", "firewalld.service")
SYSCTL_KEYS = (
    "net.ipv4.ip_forward",
    "net.ipv4.conf.all.send_redirects",
    "net.ipv4.conf.default.send_redirects",
    "net.ipv4.conf.all.accept_redirects",
    "net.ipv4.conf.default.accept_redirects",
    "net.ipv4.conf.all.accept_source_route",
    "net.ipv4.conf.default.accept_source_route",
    "net.ipv4.conf.all.rp_filter",
    "net.ipv4.conf.default.rp_filter",
    "net.ipv6.conf.all.disable_ipv6",
    "net.ipv6.conf.default.disable_ipv6",
)
ENABLED = {"enabled", "enabled-runtime"}
ACTIVE = {"active", "activating", "reloading", "deactivating"}


class Runtime:
    """Manage only this bundle's interface, sysctls, table and units."""

    def __init__(self, bundle):
        self.bundle = bundle
        self.root = Path(bundle.root)

    def _require_live_root(self):
        if self.root != Path("/"):
            raise ValueError("VPN runtime actions require the host root /")

    def _unit_state(self, unit):
        output = run(
            "systemctl",
            "show",
            unit,
            "--property=LoadState,ActiveState,UnitFileState",
        )
        values = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
        if not {"LoadState", "ActiveState", "UnitFileState"} <= set(values):
            raise ValueError(f"Could not read systemd state for {unit}")
        return {
            "load_state": values["LoadState"],
            "active_state": values["ActiveState"],
            "unit_file_state": values["UnitFileState"],
        }

    @staticmethod
    def _unit_names():
        return (
            NETWORKING,
            NFTABLES,
            *FIREWALL_MANAGERS,
            FIREWALL,
            *WG_UNITS,
        )

    def _table(self):
        data = json.loads(run("nft", "-j", "list", "tables"))
        tables = {
            f"{item['table']['family']} {item['table']['name']}"
            for item in data.get("nftables", [])
            if isinstance(item, dict) and isinstance(item.get("table"), dict)
        }
        if "inet forcad_vpn" not in tables:
            return None
        return re.sub(
            r"\bcounter\s+packets\s+\d+\s+bytes\s+\d+",
            "counter packets 0 bytes 0",
            run("nft", "list", "table", "inet", "forcad_vpn"),
            flags=re.IGNORECASE,
        )

    def _sysctl_values(self):
        values = {}
        for line in (self.bundle.directory / "sysctl.conf").read_text().splitlines():
            line = line.strip()
            if line and not line.startswith("#"):
                key, value = line.split("=", 1)
                values[key.strip()] = value.strip()
        if set(values) != set(SYSCTL_KEYS):
            raise ValueError("VPN sysctl settings do not match the expected keys")
        return values

    def snapshot(self, network_state=None):
        self._require_live_root()
        states = {unit: self._unit_state(unit) for unit in self._unit_names()}
        return {
            "role": "vpn",
            "units": states,
            "table": self._table(),
            "wireguard_interfaces": sorted(run("wg", "show", "interfaces").split()),
            "sysctls": {
                key: run("sysctl", "-n", key).strip() for key in self._sysctl_values()
            },
            "network": network_state
            if network_state is not None
            else self.network_snapshot(),
        }

    def network_snapshot(self):
        device = self.bundle.config["devices"][0]
        addresses = json.loads(run("ip", "-j", "-4", "address", "show", "dev", device))
        found = []
        for item in addresses:
            for address in item.get("addr_info", []):
                if address.get("family") == "inet":
                    found.append(f"{address['local']}/{address['prefixlen']}")
        routes = json.loads(run("ip", "-j", "-4", "route", "show", "default"))
        normalized = [
            {
                "gateway": route.get("gateway"),
                "dev": route.get("dev"),
                "metric": route.get("metric", 0),
            }
            for route in routes
        ]
        normalized.sort(
            key=lambda item: (item["dev"] or "", item["gateway"] or "", item["metric"])
        )
        return {
            "device": device,
            "addresses": sorted(found),
            "default_routes": normalized,
        }

    def expected_network(self):
        device, address = self.bundle.config["addresses"][0]
        route = next(
            route for route in self.bundle.config["routes"] if route["dst"] == "default"
        )
        return {
            "device": device,
            "addresses": [address],
            "default_routes": [
                {"gateway": route["gateway"], "dev": device, "metric": 0}
            ],
        }

    def preflight(self, before, allow_existing_table=False):
        self._require_live_root()
        if before.get("role") != "vpn":
            raise ValueError("Runtime snapshot belongs to another host role")
        if before["table"] is not None and not allow_existing_table:
            raise ValueError(
                "inet forcad_vpn already exists; inspect its owner before installing"
            )
        if before["wireguard_interfaces"] and not allow_existing_table:
            raise ValueError(
                "WireGuard interfaces are already active; inspect their owner before installing"
            )
        for manager in FIREWALL_MANAGERS:
            if before["units"][manager]["active_state"] in ACTIVE:
                raise ValueError(f"Stop the other firewall manager first: {manager}")
        networking = before["units"][NETWORKING]
        if (
            networking["load_state"] != "loaded"
            or networking["active_state"] != "active"
        ):
            raise ValueError(
                "networking.service must be installed and active before VPN setup"
            )
        if networking["unit_file_state"] in {"masked", "masked-runtime"}:
            raise ValueError("Unmask networking.service before VPN setup")
        for unit in (FIREWALL, *WG_UNITS):
            state = before["units"][unit]
            if state["unit_file_state"] in {"masked", "masked-runtime"}:
                raise ValueError(f"Unmask {unit} before VPN setup")
            if state["active_state"] in {"activating", "deactivating", "reloading"}:
                raise ValueError(f"Wait for {unit} to finish changing")

    def apply(self, before):
        self._require_live_root()
        self.preflight(before, allow_existing_table=before["table"] is not None)
        run("systemctl", "daemon-reload")
        nftables = before["units"][NFTABLES]
        if nftables["active_state"] in ACTIVE:
            run("systemctl", "stop", NFTABLES)
        if nftables["unit_file_state"] in ENABLED:
            self._disable(NFTABLES, nftables["unit_file_state"])

        firewall = self._unit_state(FIREWALL)
        if firewall["load_state"] != "loaded":
            raise ValueError(f"{FIREWALL} did not load")
        self._enable(FIREWALL, firewall["unit_file_state"])
        run(
            "systemctl",
            "reload-or-restart" if firewall["active_state"] in ACTIVE else "start",
            FIREWALL,
        )
        run("sysctl", "-p", "/etc/sysctl.d/90-forcad-vpn.conf")
        if self.network_snapshot() != self.expected_network():
            run("systemctl", "restart", NETWORKING)
        for unit in WG_UNITS:
            self._enable(unit, before["units"][unit]["unit_file_state"])
            run(
                "systemctl",
                "restart"
                if before["units"][unit]["active_state"] in ACTIVE
                else "start",
                unit,
            )
        self.verify(self.bundle.options)

    def verify(self, options=None):
        self._require_live_root()
        if self._table() is None:
            raise ValueError("VPN firewall table is not loaded")
        if self._wireguard_interfaces() != sorted(self.bundle.config["wireguard"]):
            raise ValueError("Live WireGuard interfaces differ from the bundle")
        if self.network_snapshot() != self.expected_network():
            raise ValueError(
                "VPN interface address or default route differs from the bundle"
            )
        for key, expected in self._sysctl_values().items():
            if run("sysctl", "-n", key).strip() != expected:
                raise ValueError(f"Live sysctl differs: {key}")
        for unit in (NETWORKING, FIREWALL, *WG_UNITS):
            state = self._unit_state(unit)
            if state["active_state"] != "active":
                raise ValueError(f"Service is not active: {unit}")
        for name, expected in self.bundle.config["wireguard"].items():
            unit = f"wg-quick@{name}.service"
            if self._unit_state(unit)["unit_file_state"] != "enabled":
                raise ValueError(f"Service is not enabled: {unit}")
            if run("wg", "show", name, "listen-port").strip() != str(expected["port"]):
                raise ValueError(f"Unexpected WireGuard port: {name}")
            allowed = [
                line.split(maxsplit=1)[1]
                for line in run("wg", "show", name, "allowed-ips").splitlines()
            ]
            if len(set(allowed)) != len(allowed) or set(allowed) != set(
                expected["peers"]
            ):
                raise ValueError(f"Live WireGuard peers differ: {name}")

    def stop(self):
        self._require_live_root()
        for unit in (*WG_UNITS, FIREWALL):
            if self._unit_state(unit)["active_state"] in ACTIVE:
                if unit == FIREWALL:
                    stop_firewall(self.bundle, unit)
                else:
                    run("systemctl", "stop", unit)
            state = self._unit_state(unit)["unit_file_state"]
            if state in ENABLED:
                self._disable(unit, state)
        interfaces = set(self._wireguard_interfaces())
        for unit in WG_UNITS:
            name = unit.removeprefix("wg-quick@").removesuffix(".service")
            if name in interfaces:
                run("wg-quick", "down", name)
        if self._table() is not None:
            run("nft", "delete", "table", "inet", "forcad_vpn")

    @staticmethod
    def _wireguard_interfaces():
        return sorted(run("wg", "show", "interfaces").split())

    def verify_removed(self):
        self._require_live_root()
        remaining = set(self._wireguard_interfaces()) & {"wg-attack", "wg-admin"}
        if remaining:
            raise ValueError(
                "Managed WireGuard interfaces remain active: "
                + ", ".join(sorted(remaining))
            )
        for unit in (FIREWALL, *WG_UNITS):
            state = self._unit_state(unit)
            if state["active_state"] in ACTIVE or state["unit_file_state"] in ENABLED:
                raise ValueError(
                    f"Managed VPN service remains active or enabled: {unit}"
                )

    def check_restore(self, snapshot):
        for manager in FIREWALL_MANAGERS:
            if self._unit_state(manager)["active_state"] in ACTIVE:
                raise ValueError(f"Another firewall manager is active: {manager}")
        current = self._unit_state(NFTABLES)["active_state"] in ACTIVE
        wanted = snapshot["units"][NFTABLES]["active_state"] == "active"
        if current != wanted:
            inventory = json.loads(run("nft", "-j", "list", "tables"))
            foreign = [
                f"{item['table']['family']} {item['table']['name']}"
                for item in inventory.get("nftables", [])
                if "table" in item
                and (item["table"]["family"], item["table"]["name"])
                != ("inet", "forcad_vpn")
            ]
            if foreign:
                raise ValueError(
                    "Cannot restore nftables.service with foreign tables: "
                    + ", ".join(foreign)
                )

    def restore(self, snapshot):
        self._require_live_root()
        if snapshot.get("role") != "vpn":
            raise ValueError("Runtime snapshot belongs to another host role")
        self.check_restore(snapshot)
        self.stop()
        for key, value in snapshot["sysctls"].items():
            run("sysctl", "-w", f"{key}={value}")
        run("systemctl", "daemon-reload")
        if self.network_snapshot() != snapshot["network"]:
            run("systemctl", "restart", NETWORKING)
        if self.network_snapshot() != snapshot["network"]:
            raise ValueError(
                "Could not restore the original VPN interface addresses and routes"
            )
        order = (NFTABLES, NETWORKING, *FIREWALL_MANAGERS, FIREWALL, *WG_UNITS)
        for unit in order:
            wanted = snapshot["units"][unit]
            current = self._unit_state(unit)
            if wanted["load_state"] == "not-found":
                continue
            if current["load_state"] == "not-found":
                raise ValueError(f"Cannot restore missing pre-existing unit: {unit}")
            state = current["unit_file_state"]
            target = wanted["unit_file_state"]
            self._set_unit_file_state(unit, target, state)
        for unit in order:
            wanted = snapshot["units"][unit]
            if wanted["load_state"] == "not-found":
                continue
            active = self._unit_state(unit)["active_state"] in ACTIVE
            if wanted["active_state"] == "active" and not active:
                if unit == NFTABLES:
                    self.check_restore(snapshot)
                run("systemctl", "start", unit)
            elif wanted["active_state"] != "active" and active:
                if unit == NFTABLES:
                    self.check_restore(snapshot)
                if unit == FIREWALL:
                    stop_firewall(self.bundle, unit)
                else:
                    run("systemctl", "stop", unit)
        if snapshot["table"] is not None and self._table() is None:
            with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as rules:
                rules.write(snapshot["table"])
                rules.flush()
                run("nft", "-f", rules.name)
        if self._table() != snapshot["table"]:
            raise ValueError("Could not restore the original VPN firewall table")

    @staticmethod
    def _enable(unit, state):
        if state == "enabled":
            return
        if state == "enabled-runtime":
            run("systemctl", "--runtime", "disable", unit)
        run("systemctl", "enable", unit)

    @staticmethod
    def _disable(unit, state):
        if state == "enabled-runtime":
            run("systemctl", "--runtime", "disable", unit)
        else:
            run("systemctl", "disable", unit)

    @staticmethod
    def _set_unit_file_state(unit, wanted, current):
        if wanted == current:
            return
        if current == "masked-runtime":
            run("systemctl", "--runtime", "unmask", unit)
        elif current == "masked":
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


class Lifecycle:
    """Coordinate file changes with rollback of VPN runtime state."""

    def __init__(self, bundle):
        self.bundle = bundle
        self.runtime = Runtime(bundle)
        self.journal = bundle.path("/var/backups/forcad/vpn/live.json")

    @contextmanager
    def locked(self):
        regular(self.journal)
        lock = regular(self.bundle.path("/run/lock/forcad-vpn.lock"))
        lock.parent.mkdir(parents=True, exist_ok=True)
        with lock.open("a") as stream:
            os.fchmod(stream.fileno(), 0o600)
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            yield

    def load(self):
        if not regular(self.journal).exists():
            return None
        envelope = json.loads(self.journal.read_text())
        record = envelope["record"]
        digest = hashlib.sha256(json.dumps(record, sort_keys=True).encode()).hexdigest()
        if (
            envelope.get("sha256") != digest
            or record.get("format") != 1
            or record.get("phase") != "active"
        ):
            raise ValueError(
                f"Incomplete or corrupt VPN recovery journal: {self.journal}"
            )
        return record

    def save(self, record):
        digest = hashlib.sha256(json.dumps(record, sort_keys=True).encode()).hexdigest()
        atomic_write(
            self.journal,
            (
                json.dumps({"record": record, "sha256": digest}, indent=2) + "\n"
            ).encode(),
            0o600,
            os.geteuid(),
            os.getegid(),
        )

    def checkpoint(self, paths, action):
        paths = list(dict.fromkeys([*paths, self.bundle.manifest]))
        directories = self.bundle.directory_changes(paths)
        backup, saved = self.bundle.backup(paths, action, directories)
        return backup, saved, paths, directories

    def install(self):
        with self.locked():
            previous = self.load()
            before = self.runtime.snapshot()
            if previous and before != previous["applied"]:
                raise ValueError(
                    "VPN runtime changed since installation; inspect it before reinstalling"
                )
            self.runtime.preflight(before, allow_existing_table=bool(previous))
            baseline = deepcopy(previous["baseline"] if previous else before)
            checkpoint = self.checkpoint(self.bundle.changes, "apply")
            applying = {
                "format": 1,
                "phase": "applying",
                "baseline": baseline,
                "before": before,
                "options": self.bundle.options,
            }
            try:
                self.bundle.install_files()
                self._save_incomplete(applying)
                self.runtime.apply(before)
                applied = self.runtime.snapshot()
                self.save(
                    {
                        "format": 1,
                        "phase": "active",
                        "baseline": baseline,
                        "applied": applied,
                        "options": self.bundle.options,
                    }
                )
            except BaseException as error:
                failures = []
                for action in (
                    self.runtime.stop,
                    lambda: rollback_files(
                        self.bundle.root,
                        checkpoint[0],
                        checkpoint[1],
                        checkpoint[2],
                        checkpoint[3],
                    ),
                    lambda: self.runtime.restore(before),
                ):
                    try:
                        action()
                    except Exception as failure:  # noqa: BLE001 - attempt every rollback action
                        failures.append(str(failure))
                if failures:
                    raise ValueError(
                        f"VPN setup failed: {error}. Recovery incomplete: {'; '.join(failures)}. Keep {self.journal} and backups."
                    ) from error
                if previous:
                    self.save(previous)
                else:
                    self.journal.unlink(missing_ok=True)
                raise ValueError(
                    f"VPN setup failed; files and live state restored: {error}"
                ) from error
        print("PASS VPN settings applied, WireGuard tunnels and firewall are running.")

    def _save_incomplete(self, record):
        digest = hashlib.sha256(json.dumps(record, sort_keys=True).encode()).hexdigest()
        atomic_write(
            self.journal,
            (
                json.dumps({"record": record, "sha256": digest}, indent=2) + "\n"
            ).encode(),
            0o600,
            os.geteuid(),
            os.getegid(),
        )

    @staticmethod
    def removal_snapshot(snapshot):
        """Restore shared host settings while keeping the managed VPN removed."""
        target = deepcopy(snapshot)
        for unit in WG_UNITS:
            if unit in target["units"]:
                target["units"][unit].update(
                    active_state="inactive", unit_file_state="disabled"
                )
        if FIREWALL in target["units"]:
            target["units"][FIREWALL].update(
                load_state="not-found", active_state="inactive", unit_file_state=""
            )
        target["table"] = None
        if "wireguard_interfaces" in target:
            target["wireguard_interfaces"] = [
                name
                for name in target["wireguard_interfaces"]
                if name not in {"wg-attack", "wg-admin"}
            ]
        return target

    def uninstall(self, force=False, dry_run=False):
        with self.locked():
            previous = self.load()
            if previous is None and self.bundle.manifest.exists():
                current = self.runtime.snapshot()
                active = any(
                    current["units"][unit]["active_state"] in ACTIVE
                    for unit in (FIREWALL, *WG_UNITS)
                ) or bool(current["wireguard_interfaces"])
                if active or current["table"] is not None:
                    raise ValueError(
                        "VPN services are active without a recovery record; stop and inspect them before uninstalling"
                    )
            plan = self.bundle.preflight_uninstall(force=force)
            if previous is None:
                self.bundle.uninstall_files(force=force, dry_run=dry_run)
                self.bundle.finish_uninstall(force=force, dry_run=dry_run)
                print(
                    "No recorded live VPN installation; live settings were not changed."
                )
                return
            if dry_run:
                self.bundle.uninstall_files(force=force, dry_run=True)
                print(
                    "Would stop VPN services and restore the original network and sysctl state."
                )
                return
            current = self.runtime.snapshot()
            target = self.removal_snapshot(previous["baseline"])
            self.runtime.check_restore(target)
            if current["network"] != previous["applied"]["network"] and not force:
                raise ValueError(
                    "VPN network changed since installation; inspect it or use --force"
                )
            if not plan["installed"]:
                raise ValueError("Live VPN installation has no file manifest")
            paths = [self.bundle.path(item["path"]) for item in plan["actions"]]
            checkpoint = self.bundle.backup(paths, "apply", plan["directories"])
            atomic_write(
                checkpoint[0] / "live-before.json",
                (json.dumps(current, indent=2) + "\n").encode(),
                0o600,
                os.geteuid(),
                os.getegid(),
            )
            try:
                self.runtime.stop()
                self.bundle.uninstall_files(force=force)
                self.runtime.restore(target)
                self.runtime.verify_removed()
                self.journal.unlink(missing_ok=True)
            except BaseException as error:
                failures = []
                try:
                    rollback_files(
                        self.bundle.root,
                        checkpoint[0],
                        checkpoint[1],
                        paths,
                        plan["directories"],
                    )
                except Exception as recovery_error:  # noqa: BLE001
                    failures.append(str(recovery_error))
                try:
                    self.runtime.restore(current)
                except Exception as recovery_error:  # noqa: BLE001
                    failures.append(str(recovery_error))
                if failures:
                    raise ValueError(
                        f"VPN uninstall failed: {error}. Recovery incomplete: {'; '.join(failures)}. Keep {self.journal} and backups."
                    ) from error
                self.save(previous)
                raise
            self.bundle.finish_uninstall(force=force)
        print(
            "PASS managed VPN tunnels, services and files removed; shared host settings restored."
        )

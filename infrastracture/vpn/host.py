"""Plan and check VPN host files."""

import hashlib
import json
import re
import stat
import subprocess
from pathlib import Path

from vpnlib.lifecycle import Lifecycle
from vpnlib.observers import (
    ENDPOINTS_SERVICE,
    HISTORY_SCRIPT,
    HISTORY_SERVICE,
    HISTORY_SOCKET,
    INVENTORY_FILE,
    OBSERVER_SCRIPT,
    PCAP_SERVICE,
    inventory_document,
)

from hostlib.base import BundleBase
from hostlib.cli import main as run_cli
from hostlib.common import check_wireguard, regular, run
from hostlib.packages import Dependencies

OBSERVER_FILES = {
    "/etc/forcad/vpn/observer-inventory.json",
    "/etc/systemd/system/forcad-vpn-pcap.service",
    "/etc/systemd/system/forcad-wg-endpoints.service",
    "/etc/systemd/system/forcad-wg-history.socket",
    "/etc/systemd/system/forcad-wg-history.service",
    "/usr/lib/forcad/vpn/vpnlib/observers.py",
    "/usr/lib/forcad/vpn/vpnlib/history.py",
}
OBSERVER_UNITS = (
    ("forcad-wg-history.socket", "/etc/systemd/system/forcad-wg-history.socket"),
    ("forcad-wg-history.service", "/etc/systemd/system/forcad-wg-history.service"),
    ("forcad-wg-endpoints.service", "/etc/systemd/system/forcad-wg-endpoints.service"),
    ("forcad-vpn-pcap.service", "/etc/systemd/system/forcad-vpn-pcap.service"),
)


class Bundle(BundleBase):
    ROLE = "vpn"

    def observer_contents(self, paths):
        contents = {}
        if INVENTORY_FILE in paths:
            contents[INVENTORY_FILE] = inventory_document(self.config["teams"]).encode()
        if OBSERVER_SCRIPT in paths:
            contents[OBSERVER_SCRIPT] = regular(
                self.directory / "vpnlib/observers.py"
            ).read_bytes()
        if HISTORY_SCRIPT in paths:
            contents[HISTORY_SCRIPT] = regular(
                self.directory / "vpnlib/history.py"
            ).read_bytes()
        units = {
            "/etc/systemd/system/forcad-vpn-pcap.service": PCAP_SERVICE,
            "/etc/systemd/system/forcad-wg-endpoints.service": ENDPOINTS_SERVICE,
            "/etc/systemd/system/forcad-wg-history.socket": HISTORY_SOCKET,
            "/etc/systemd/system/forcad-wg-history.service": HISTORY_SERVICE,
        }
        contents.update(
            {path: unit.encode() for path, unit in units.items() if path in paths}
        )
        return contents

    def install_files(self, *, extend=False):
        if not extend and self.manifest.exists():
            previous = json.loads(regular(self.manifest).read_text())
            observer_items = [
                item
                for item in previous.get("files", [])
                if item.get("path") in OBSERVER_FILES
            ]
            contents = self.observer_contents(
                {item.get("path") for item in observer_items}
            )
            for item in observer_items:
                name = item["path"]
                path = regular(self.path(name))
                try:
                    data = path.read_bytes()
                except OSError:
                    raise ValueError(
                        "An installed VPN observer file is missing; repair or uninstall observers before reinstalling."
                    ) from None
                info = path.stat()
                if (
                    hashlib.sha256(data).hexdigest() != item.get("sha256")
                    or stat.S_IMODE(info.st_mode) != item.get("mode")
                ):
                    raise ValueError(
                        "An installed VPN observer file changed; inspect it before reinstalling."
                    )
                self.put(name, contents.get(name, data), item["mode"])
        return super().install_files(extend=extend)

    def _require_base_receipt(self):
        try:
            receipt = json.loads(regular(self.manifest).read_text())
        except (OSError, ValueError):
            raise ValueError(
                "Install the base VPN bundle before installing observers."
            ) from None
        config = receipt.get("config") if isinstance(receipt, dict) else None
        options = receipt.get("options") if isinstance(receipt, dict) else None
        if (
            not isinstance(receipt, dict)
            or receipt.get("format") != 2
            or not isinstance(config, dict)
            or config.get("role") != "vpn"
            or config != self.config
            or not isinstance(options, dict)
        ):
            raise ValueError(
                "Install the matching base VPN bundle before installing observers."
            )
        self.options = options.copy()
        if self.options.get("vpn_live") and not self.files_only:
            if Lifecycle(self).load() is None:
                raise ValueError(
                    "The live VPN recovery record is missing; repair the base installation first."
                )
        return receipt

    def _assert_observer_paths_available(self, receipt, paths):
        owned = {
            item["path"]: item
            for item in receipt.get("files", [])
            if isinstance(item, dict) and "path" in item
        }
        for name in paths:
            path = regular(self.path(name))
            if not path.exists():
                continue
            record = owned.get(name)
            if record is None:
                raise ValueError(
                    "An observer destination already exists and is not owned by this VPN installation."
                )
            data = path.read_bytes()
            info = path.stat()
            if (
                hashlib.sha256(data).hexdigest() != record.get("sha256")
                or stat.S_IMODE(info.st_mode) != record.get("mode")
            ):
                raise ValueError(
                    "An installed VPN observer file changed; inspect it before reinstalling."
                )

    def install_observers(self, *, pcap: bool, wg_endpoints: bool):
        if not pcap and not wg_endpoints:
            return
        receipt = self._require_base_receipt()
        paths = {INVENTORY_FILE, OBSERVER_SCRIPT}
        if pcap:
            paths.add("/etc/systemd/system/forcad-vpn-pcap.service")
        if wg_endpoints:
            paths.update(
                {
                    HISTORY_SCRIPT,
                    "/etc/systemd/system/forcad-wg-endpoints.service",
                    "/etc/systemd/system/forcad-wg-history.socket",
                    "/etc/systemd/system/forcad-wg-history.service",
                }
            )
        self._assert_observer_paths_available(receipt, paths)
        if pcap and not self.files_only and self.root == Path("/"):
            Dependencies(self).install_monitoring(pcap=True)
        previous_changes = self.changes
        self.changes = {}
        try:
            self.put(
                INVENTORY_FILE,
                inventory_document(self.config["teams"]).encode(),
                0o600,
            )
            observer_source = regular(self.directory / "vpnlib/observers.py")
            self.put(OBSERVER_SCRIPT, observer_source.read_bytes(), 0o644)
            if pcap:
                self.put(
                    "/etc/systemd/system/forcad-vpn-pcap.service",
                    PCAP_SERVICE.encode(),
                    0o644,
                )
            if wg_endpoints:
                history_source = regular(self.directory / "vpnlib/history.py")
                self.put(HISTORY_SCRIPT, history_source.read_bytes(), 0o644)
                self.put(
                    "/etc/systemd/system/forcad-wg-endpoints.service",
                    ENDPOINTS_SERVICE.encode(),
                    0o644,
                )
                self.put(
                    "/etc/systemd/system/forcad-wg-history.socket",
                    HISTORY_SOCKET.encode(),
                    0o644,
                )
                self.put(
                    "/etc/systemd/system/forcad-wg-history.service",
                    HISTORY_SERVICE.encode(),
                    0o644,
                )
            self.install_files(extend=True)
        finally:
            self.changes = previous_changes
        if self.root == Path("/") and not self.files_only:
            run("systemctl", "daemon-reload")

    def _owned_observer_units(self):
        if not self.manifest.exists():
            return []
        receipt = json.loads(regular(self.manifest).read_text())
        paths = {item.get("path") for item in receipt.get("files", [])}
        return [unit for unit, path in OBSERVER_UNITS if path in paths]

    def stop_observer_services(self):
        if self.files_only or self.root != Path("/"):
            return {}
        states = {}
        for unit in self._owned_observer_units():
            result = subprocess.run(
                ["systemctl", "is-active", unit],
                capture_output=True,
                text=True,
                check=False,
                timeout=30,
            )
            states[unit] = result.stdout.strip() in {
                "active",
                "activating",
                "reloading",
                "deactivating",
            }
        try:
            for unit, active in states.items():
                if active:
                    run("systemctl", "stop", unit)
        except BaseException:
            self.restore_observer_services(states)
            raise
        return states

    @staticmethod
    def restore_observer_services(states):
        for unit, active in reversed(tuple(states.items())):
            if active:
                run("systemctl", "start", unit)

    def uninstall_owned_paths(self):
        return {
            *OBSERVER_FILES,
            "/etc/sysctl.d/90-forcad-vpn.conf",
            "/etc/systemd/system/forcad-vpn-firewall.service",
            "/etc/systemd/system/networking.service.d/forcad.conf",
            "/etc/forcad/vpn/firewall.nft",
            *(
                path
                for name in ("wg-attack", "wg-admin")
                for path in (
                    f"/etc/wireguard/{name}.conf",
                    f"/etc/systemd/system/wg-quick@{name}.service.d/firewall.conf",
                )
            ),
        }



    def finish_uninstall(self, force=False, dry_run=False):
        if not dry_run and getattr(self, "removed_managed_files", False):
            for name in (
                "/etc/wireguard",
                "/etc/forcad/vpn",
                "/etc/forcad",
                "/etc/systemd/system/wg-quick@wg-attack.service.d",
                "/etc/systemd/system/wg-quick@wg-admin.service.d",
                "/etc/systemd/system/networking.service.d",
            ):
                path = self.path(name)
                regular(path / "check")
                if path.is_dir() and not any(path.iterdir()):
                    path.rmdir()
        super().finish_uninstall(force=force, dry_run=dry_run)
        if (
            not dry_run
            and self.root == Path("/")
            and getattr(self, "removed_observer_files", False)
        ):
            run("systemctl", "daemon-reload")

    def uninstall_files(self, force=False, dry_run=False):
        installed = self.manifest.exists()
        result = super().uninstall_files(force=force, dry_run=dry_run)
        self.removed_managed_files = installed and not dry_run
        return result

    def has_live_installation(self):
        return Lifecycle(self).load() is not None

    def install(self):
        if self.files_only:
            if self.has_live_installation():
                raise ValueError(
                    "VPN has a live installation; rerun without file-only mode"
                )
            return super().install()
        self.options["vpn_live"] = True
        return Lifecycle(self).install()

    def uninstall(self, force=False, dry_run=False):
        if self.files_only:
            if self.has_live_installation():
                raise ValueError(
                    "VPN has a live installation; rerun without file-only mode"
                )
            return super().uninstall(force=force, dry_run=dry_run)
        self.removed_observer_files = bool(self._owned_observer_units())
        return Lifecycle(self).uninstall(force=force, dry_run=dry_run)

    def test(self, live=False):
        super().test(live=live or not self.files_only)

    def plan_role(self, repo, wireguard_dir, network_metrics, game_metrics):
        base = "/etc/forcad/vpn"
        unit = "forcad-vpn-firewall.service"
        self.copy("sysctl.conf", "/etc/sysctl.d/90-forcad-vpn.conf", 0o644)
        self.copy(unit, f"/etc/systemd/system/{unit}", 0o644)
        self.copy(
            "networking.conf",
            "/etc/systemd/system/networking.service.d/forcad.conf",
            0o644,
        )
        self.copy("firewall.nft", f"{base}/firewall.nft")
        for name in ("wg-attack", "wg-admin"):
            self.copy(
                "firewall.conf",
                f"/etc/systemd/system/wg-quick@{name}.service.d/firewall.conf",
                0o644,
            )
        candidates = (
            [Path(wireguard_dir)]
            if wireguard_dir
            else [
                self.directory / "server",
                self.directory.parent / "server",
                self.directory.parent / "wireguard/server",
            ]
        )
        server = next((path for path in candidates if path.is_dir()), None)
        for name in ("wg-attack", "wg-admin"):
            target = self.path(f"/etc/wireguard/{name}.conf")
            if server:
                content = regular(server / f"{name}.conf").read_bytes()
                if target.exists():
                    pattern = rb"(?m)^\s*PrivateKey\s*=\s*(\S+)\s*$"
                    old_keys = re.findall(pattern, regular(target).read_bytes())
                    new_keys = re.findall(pattern, content)
                    if len(old_keys) != 1 or new_keys != old_keys:
                        raise ValueError(
                            f"Refusing to replace the existing {name} private key"
                        )
                self.put(f"/etc/wireguard/{name}.conf", content)
            elif target.exists():
                self.put(f"/etc/wireguard/{name}.conf", regular(target).read_bytes())
            else:
                raise ValueError(
                    "Use the complete VPN bundle from render.py, or pass existing server configs with --wireguard-dir PATH."
                )
            check_wireguard(self.changes[target][0], self.config["wireguard"][name])

    def validate_role(self, work):
        firewall = self.changes[self.path("/etc/forcad/vpn/firewall.nft")][0]
        (work / "firewall.nft").write_bytes(firewall)
        run("nft", "-c", "-f", str(work / "firewall.nft"))
        for name in ("wg-attack", "wg-admin"):
            path = work / f"{name}.conf"
            path.write_bytes(self.changes[self.path(f"/etc/wireguard/{name}.conf")][0])
            path.chmod(0o600)
            check_wireguard(path.read_bytes(), self.config["wireguard"][name])
            run("wg-quick", "strip", str(path))

    def test_live_role(self):
        Lifecycle(self).runtime.verify(self.options)
        print(
            "NOTE Client handshakes and access isolation need a connected VPN client."
        )


def main():
    run_cli(Bundle, Path(__file__).resolve().parent)


if __name__ == "__main__":
    main()

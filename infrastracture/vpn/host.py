"""Plan and check VPN host files."""

import re
from pathlib import Path

from vpnlib.lifecycle import Lifecycle

from hostlib.base import BundleBase
from hostlib.cli import main as run_cli
from hostlib.common import check_wireguard, regular, run


class Bundle(BundleBase):
    ROLE = "vpn"

    def uninstall_owned_paths(self):
        return {
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

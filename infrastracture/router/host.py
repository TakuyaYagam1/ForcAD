"""Plan and check router host files."""

import json
import tempfile
from pathlib import Path

from routerlib.lifecycle import Lifecycle

from hostlib.base import BundleBase
from hostlib.cli import main as run_cli
from hostlib.common import run


class Bundle(BundleBase):
    ROLE = "router"

    def has_live_installation(self):
        if self.path("/var/backups/forcad/router/live.json").exists():
            return True
        return self.manifest.exists() and json.loads(self.manifest.read_text())[
            "options"
        ].get("router_live", False)

    def install(self):
        if self.files_only:
            if self.has_live_installation():
                raise ValueError(
                    "Router has a live installation; rerun without --files-only"
                )
            return super().install()
        self.options["router_live"] = True
        return Lifecycle(self).install()

    def uninstall(self, force=False, dry_run=False, resume=False):
        if self.files_only:
            if resume:
                raise ValueError("--resume requires live router uninstall")
            if self.has_live_installation():
                raise ValueError(
                    "Router has a live installation; rerun without --files-only"
                )
            return super().uninstall(force=force, dry_run=dry_run)
        return Lifecycle(self).uninstall(force=force, dry_run=dry_run, resume=resume)

    def test(self, live=False):
        super().test(live=live or not self.files_only)
        if live or not self.files_only:
            Lifecycle(self).system.verify(self.options)

    def plan_role(self, repo, wireguard_dir, network_metrics, game_metrics):
        base = "/etc/forcad/router"
        unit = "forcad-router-firewall.service"
        self.copy("sysctl.conf", "/etc/sysctl.d/90-forcad-router.conf", 0o644)
        self.copy(unit, f"/etc/systemd/system/{unit}", 0o644)
        self.copy(
            "networking.conf",
            "/etc/systemd/system/networking.service.d/forcad.conf",
            0o644,
        )
        for name in ("common.nft", "game.nft", "pre-game.nft"):
            self.copy(name, f"{base}/{name}")
        active = self.path(f"{base}/active.nft")
        if active.exists():
            content = self.path(f"{base}/active.nft").read_bytes()
            if content.strip() not in (
                b'include "/etc/forcad/router/game.nft"',
                b'include "/etc/forcad/router/pre-game.nft"',
            ):
                raise ValueError(
                    "Existing active.nft contains custom rules; preserve and review them first"
                )
            self.put(f"{base}/active.nft", content)
        else:
            self.copy("active.nft", f"{base}/active.nft")
        self._plan_monitoring(network_metrics, game_metrics)

    def _plan_monitoring(self, network_metrics, game_metrics):
        if network_metrics:
            self.copy("network.json", "/etc/forcad/monitoring/network.json", 0o644)
            self.copy("network.nft", "/etc/forcad/monitoring/network.nft")
            json.loads((self.directory / "network.json").read_text())
            self.copy("forcad", "/usr/local/bin/forcad", 0o755)
            for name in ("network-rules.service", "network-metrics.service"):
                self.copy(name, f"/etc/systemd/system/{name}", 0o644)
        if game_metrics:
            for name in ("game-metrics.zeek", "game-inventory.zeek"):
                self.copy(name, f"/etc/forcad/monitoring/{name}", 0o644)
            self.copy(
                "game-metrics.service",
                "/etc/systemd/system/game-metrics.service",
                0o644,
            )

    def plan_monitoring(self):
        self.changes.clear()
        self._plan_monitoring(network_metrics=True, game_metrics=True)

    def _validate_monitoring_rules(self, work):
        if not self.options["network_metrics"]:
            return
        rules = work / "network.nft"
        rules.write_bytes(
            b"add table inet forcad_monitoring\ndelete table inet forcad_monitoring\n"
            + self.changes[self.path("/etc/forcad/monitoring/network.nft")][0]
        )
        run("nft", "-c", "-f", str(rules))

    def validate_monitoring(self):
        with tempfile.TemporaryDirectory(prefix="forcad-monitoring-check-") as directory:
            self._validate_monitoring_rules(Path(directory))
        if self.options["game_metrics"]:
            zeek = Path("/opt/zeek/bin/zeek")
            if not zeek.is_file():
                raise ValueError("Install Zeek at /opt/zeek before using monitoring")
            run(
                str(zeek),
                "-b",
                "-a",
                str(self.directory / "game-metrics.zeek"),
                str(self.directory / "game-inventory.zeek"),
            )

    def validate_role(self, work):
        for path, (data, _) in self.changes.items():
            if path.suffix == ".nft" and path.parent == Path("/etc/forcad/router"):
                (work / path.name).write_bytes(
                    data.replace(b"/etc/forcad/router/", (str(work) + "/").encode())
                )
        run("nft", "-c", "-f", str(work / "active.nft"))
        self._validate_monitoring_rules(work)

    def install_monitoring(self):
        if self.files_only or not self.manifest.exists():
            raise ValueError("Monitoring requires a live router installation")
        return Lifecycle(self).install_monitoring()

    def test_live_role(self):
        run(
            "systemctl",
            "is-active",
            "--quiet",
            "forcad-router-firewall.service",
        )
        run("nft", "list", "table", "inet", "forcad_router")
        for line in (
            self.path("/etc/sysctl.d/90-forcad-router.conf").read_text().splitlines()
        ):
            key, value = line.split("=", 1)
            if run("sysctl", "-n", key).strip() != value:
                raise ValueError(f"Live sysctl differs: {key}")
        run("nft", "list", "table", "ip", "forcad_nat")


def main():
    run_cli(Bundle, Path(__file__).resolve().parent)


if __name__ == "__main__":
    main()

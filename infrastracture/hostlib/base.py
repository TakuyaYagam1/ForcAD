"""Common planning, validation and installed-file checks."""

import glob
import hashlib
import json
import re
import stat
import tempfile
from pathlib import Path

from .checks import http
from .common import read_yaml, regular, run
from .files import FileOperations


class BundleBase(FileOperations):
    """Base interface shared by role-specific host bundles."""

    ROLE = None
    http = staticmethod(http)

    def __init__(self, directory, root=Path("/")):
        self.directory = Path(directory)
        self.root = Path(root)
        self.config = json.loads((self.directory / "host.json").read_text())
        self.role = self.config["role"]
        if self.role not in {"router", "vpn", "forcad"}:
            raise ValueError("Unknown host role")
        if self.ROLE and self.role != self.ROLE:
            raise ValueError(f"Expected a {self.ROLE} host bundle")
        self.changes = {}
        self.options = {}
        self.files_only = False
        self.manifest = self.path(f"/etc/forcad/{self.role}/install.json")

    def path(self, absolute):
        path = Path(absolute)
        if not path.is_absolute() or ".." in path.parts:
            raise ValueError("Expected an absolute installation path")
        return self.root / str(path).lstrip("/")

    def put(self, destination, data, mode=0o600):
        target = regular(self.path(destination))
        self.changes[target] = (data, mode)

    def copy(self, name, destination, mode=0o600):
        self.put(destination, regular(self.directory / name).read_bytes(), mode)

    def check_role(self):
        for role in {"router", "vpn", "forcad"} - {self.role}:
            if regular(self.path(f"/etc/forcad/{role}/install.json")).exists():
                raise ValueError(
                    f"The {role} installation already owns shared host settings; use separate hosts"
                )

    def plan(
        self, repo=None, wireguard_dir=None, network_metrics=False, game_metrics=False
    ):
        self.check_role()
        self.options = {
            "repo": str(repo) if repo else None,
            "network_metrics": network_metrics,
            "game_metrics": game_metrics,
        }
        self.plan_interfaces()
        self.plan_role(repo, wireguard_dir, network_metrics, game_metrics)

    def plan_role(self, repo, wireguard_dir, network_metrics, game_metrics):
        raise NotImplementedError("Role bundle must implement plan_role()")

    def plan_interfaces(self):
        self.copy("interfaces", "/etc/network/interfaces", 0o644)
        current = self.path("/etc/network/interfaces")
        if current.exists():
            generated = self.changes[current][0]
            expected = set(re.findall(rb"(?m)^iface\s+(\S+)", generated))
            old = regular(current).read_bytes()
            extra = set(re.findall(rb"(?m)^\s*iface\s+(\S+)", old)) - expected
            if extra:
                raise ValueError(
                    "Existing interfaces contain extra devices; merge them manually before installation"
                )
            includes = []
            for line in old.decode().splitlines():
                fields = line.split()
                if fields and fields[0] in {"source", "source-directory"}:
                    for pattern in fields[1:]:
                        pattern = (
                            str(self.path(pattern))
                            if pattern.startswith("/")
                            else str(current.parent / pattern)
                        )
                        for match in glob.glob(pattern):
                            paths = (
                                Path(match).iterdir()
                                if Path(match).is_dir()
                                else [Path(match)]
                            )
                            for path in paths:
                                if any(
                                    text.strip() and not text.lstrip().startswith("#")
                                    for text in regular(path).read_text().splitlines()
                                ):
                                    raise ValueError(
                                        f"Included network configuration needs a manual merge: {path}"
                                    )
                    includes.append(line)
            if includes:
                self.put(
                    "/etc/network/interfaces",
                    ("\n".join(includes) + "\n").encode() + generated,
                    0o644,
                )

    def validate(self):
        if self.root != Path("/"):
            raise ValueError("Host validation requires the real filesystem")
        for name in self.config["devices"]:
            if not (Path("/sys/class/net") / name).exists():
                raise ValueError(f"Network device does not exist on this host: {name}")
        current = self.path("/etc/network/interfaces")
        if current.exists():
            configured = set(
                run("ifquery", "--interfaces", str(current), "--list").splitlines()
            )
            expected = {
                "lo",
                *self.config["devices"],
                *[name for name, _ in self.config["addresses"]],
            }
            extra = configured - expected
            if extra:
                raise ValueError(
                    f"Existing interfaces need a manual merge: {', '.join(sorted(extra))}"
                )
        with tempfile.TemporaryDirectory(prefix="forcad-check-") as directory:
            work = Path(directory)
            (work / "interfaces").write_bytes(
                self.changes[self.path("/etc/network/interfaces")][0]
            )
            run("ifquery", "--interfaces", str(work / "interfaces"), "--list")
            self.validate_role(work)
        if self.role == "router" and self.options["game_metrics"]:
            if not Path("/opt/zeek/bin/zeek").is_file():
                raise ValueError(
                    "Install Zeek at /opt/zeek before using --game-metrics"
                )
            run(
                "/opt/zeek/bin/zeek",
                "-b",
                "-a",
                str(self.directory / "game-metrics.zeek"),
                str(self.directory / "game-inventory.zeek"),
            )

    def validate_role(self, work):
        raise NotImplementedError("Role bundle must implement validate_role()")

    def install(self):
        result = self.install_files()
        if self.role != "router" or self.files_only:
            print("Network settings and services have not been applied. See APPLY.txt.")
        return result

    def uninstall(self, force=False, dry_run=False):
        result = self.uninstall_files(force=force, dry_run=dry_run)
        print("Live network settings and services are unchanged. See UNINSTALL.txt.")
        return result

    def test_files(self, live=False):
        installed = json.loads(regular(self.manifest).read_text())
        if installed["config"] != self.config:
            raise ValueError(
                "Installed inventory differs from this bundle; run install.sh first"
            )
        for item in installed["files"]:
            path = regular(self.path(item["path"]))
            data = path.read_bytes()
            active = item[
                "path"
            ] == "/etc/forcad/router/active.nft" and data.strip() in (
                b'include "/etc/forcad/router/game.nft"',
                b'include "/etc/forcad/router/pre-game.nft"',
            )
            if not active and hashlib.sha256(data).hexdigest() != item["sha256"]:
                raise ValueError(f"Installed file differs: {path}")
            if stat.S_IMODE(path.stat().st_mode) != item["mode"]:
                raise ValueError(f"Unexpected permissions: {path}")
        self.options = installed["options"]
        self.changes = {
            self.path(item["path"]): (
                self.path(item["path"]).read_bytes(),
                item["mode"],
            )
            for item in installed["files"]
        }
        if live:
            self.test_live()

    def test(self, live=False):
        self.test_files()
        self.validate()
        print("PASS installed files, permissions and configuration syntax")
        if live:
            self.test_live()
        else:
            print(
                "Live state was not checked. After applying, run: bash test.sh --live"
            )

    def test_live(self):
        addresses = json.loads(run("ip", "-j", "-4", "address", "show"))
        observed = {
            (device["ifname"], f"{address['local']}/{address['prefixlen']}")
            for device in addresses
            for address in device.get("addr_info", [])
        }
        for device, address in self.config["addresses"]:
            if (device, address) not in observed:
                raise ValueError(f"Missing live address: {device} {address}")
        routes = json.loads(run("ip", "-j", "-4", "route", "show"))
        for expected in self.config["routes"]:
            if not any(
                all(route.get(key) == value for key, value in expected.items())
                for route in routes
            ):
                raise ValueError(f"Missing live route: {expected}")
        for address in self.config["ping"]:
            run("ping", "-n", "-c", "1", "-W", "2", address)
        self.test_live_role()
        for enabled, unit, port in (
            (self.options["network_metrics"], "network-metrics", 9109),
            (self.options["game_metrics"], "game-metrics", 9911),
        ):
            if not enabled:
                continue
            if self.role == "router":
                run("systemctl", "is-active", "--quiet", unit + ".service")
            else:
                name = "routers.yml" if port == 9109 else "game-sensors.yml"
                discovery = Path(self.options["repo"]) / "deployment/monitoring/file_sd"
                groups = read_yaml(discovery / name)
                if not any(
                    f"{self.config['checks']['metrics_address']}:{port}"
                    in group.get("targets", [])
                    for group in groups
                ):
                    raise ValueError(f"Monitoring target is not applied: {name}")
            http(
                f"http://{self.config['checks']['metrics_address']}:{port}/metrics",
                metrics=unit,
            )
        print("PASS live addresses, routes, connectivity and selected services")
        print("NOTE Run game/pre-game, SNAT, checker and load checks separately.")

    def test_live_role(self):
        pass

"""Install Debian host dependencies and keep their ownership outside rollback history."""

import base64
import contextlib
import hashlib
import json
import os
import re
import shutil
import subprocess
from pathlib import Path

from .common import regular
from .files import atomic_write

PACKAGE = re.compile(r"[a-z0-9][a-z0-9.+-]*(?::[a-z0-9_-]+)?\Z")
POLICY = b"#!/bin/sh\n# ForcAD package installation: keep live services unchanged.\nexit 101\n"
SERVICES = ("nftables.service", "networking.service", "resolvconf.service")
BASE = [
    "python3",
    "nftables",
    "iproute2",
    "iputils-ping",
    "procps",
    "ifupdown",
    "resolvconf",
]


class Dependencies:
    def __init__(self, bundle):
        self.bundle = bundle
        self.role = bundle.role
        self.path = bundle.path
        self.directory = self.path(f"/var/lib/forcad-dependencies/{self.role}")
        self.owner = f"forcad-dependencies-v1:{self.role}\n"
        self.data = None
        self.architecture = None

    @staticmethod
    def run(*args, **kwargs):
        return subprocess.run(
            args,
            check=kwargs.pop("check", True),
            text=kwargs.pop("text", True),
            env=dict(
                os.environ,
                LC_ALL="C",
                DEBIAN_FRONTEND="noninteractive",
                NEEDRESTART_MODE="l",
            ),
            **kwargs,
        )

    def _read(self):
        regular(self.directory / "owner")
        if not self.directory.exists():
            return False
        if (self.directory / "owner").read_text() != self.owner:
            raise ValueError(f"Unrecognized dependency state: {self.directory}")
        state = regular(self.directory / "state.json")
        self.data = (
            json.loads(state.read_text())
            if state.exists()
            else {
                "format": 1,
                "role": self.role,
                "packages": [],
                "files": {},
                "directories": [],
                "accounts": {},
            }
        )
        if self.data.get("format") != 1 or self.data.get("role") != self.role:
            raise ValueError("Invalid dependency ownership record")
        if any(not PACKAGE.fullmatch(name) for name in self.data["packages"]):
            raise ValueError("Invalid package name in dependency record")
        return True

    def save(self):
        atomic_write(
            self.directory / "state.json",
            (json.dumps(self.data, indent=2) + "\n").encode(),
            0o600,
            0,
            0,
        )

    def _open(self):
        if not self._read():
            self.directory.mkdir(mode=0o700, parents=True, exist_ok=False)
            atomic_write(self.directory / "owner", self.owner.encode(), 0o600, 0, 0)
            self._read()
        self._recover_policy()
        before = regular(self.directory / "bootstrap-before")
        candidates = regular(self.directory / "bootstrap-candidates")
        if before.exists():
            if candidates.exists():
                self._track(
                    set(before.read_text().splitlines()),
                    set(candidates.read_text().splitlines()),
                )
            before.unlink()
        candidates.unlink(missing_ok=True)
        if "pending" in self.data:
            self._finish_install()
        self.save()

    def _package_states(self):
        output = self.run(
            "dpkg-query",
            "-W",
            "-f=${db:Status-Status} ${binary:Package}\n",
            capture_output=True,
        ).stdout
        return {
            fields[1]: fields[0]
            for line in output.splitlines()
            if len(fields := line.split()) == 2
        }

    def installed(self):
        return {
            name
            for name, state in self._package_states().items()
            if state == "installed"
        }

    def present(self):
        # Residual conffiles belong to the host, even when the package is removed.
        return {
            name
            for name, state in self._package_states().items()
            if state != "not-installed"
        }

    def _qualified(self, names):
        if self.architecture is None:
            self.architecture = self.run(
                "dpkg", "--print-architecture", capture_output=True
            ).stdout.strip()
        if any(not PACKAGE.fullmatch(name) for name in names):
            raise ValueError("Invalid dependency package name")
        return {
            name if ":" in name else f"{name}:{self.architecture}" for name in names
        }

    def _track(self, before, candidates):
        allowed = self._qualified(candidates) - self._qualified(before)
        current = self.present()
        added = {name for name in current if self._qualified([name]) <= allowed}
        self.data["packages"] = sorted(set(self.data["packages"]) | added)
        self.save()

    def own_file(self, name, data, mode=0o644):
        path = regular(self.path(name))
        digest = hashlib.sha256(data).hexdigest()
        previous = self.data["files"].get(name)
        if path.exists():
            current = hashlib.sha256(path.read_bytes()).hexdigest()
            if previous != current:
                raise ValueError(
                    f"Dependency file belongs to another installation: {name}"
                )
        for parent in reversed(path.parents):
            if not parent.exists():
                self.data["directories"].append(
                    "/" + str(parent.relative_to(self.bundle.root))
                )
                self.save()
                parent.mkdir(mode=0o755)
        # Record intent before writing so interruption can be retried.
        self.data["files"][name] = digest
        self.save()
        atomic_write(path, data, mode, 0, 0)

    def _unit(self, name):
        result = self.run(
            "systemctl",
            "show",
            name,
            "-p",
            "LoadState",
            "-p",
            "ActiveState",
            "-p",
            "UnitFileState",
            capture_output=True,
            check=False,
        )
        return dict(
            line.split("=", 1) for line in result.stdout.splitlines() if "=" in line
        )

    def _recover_policy(self):
        saved = self.data.get("policy")
        if saved is None:
            return
        path = regular(self.path("/usr/sbin/policy-rc.d"))
        if (
            path.exists()
            and path.read_bytes() != POLICY
            and (
                saved["data"] is None
                or path.read_bytes() != base64.b64decode(saved["data"])
            )
        ):
            raise ValueError("policy-rc.d changed during dependency installation")
        if saved["data"] is None:
            path.unlink(missing_ok=True)
        else:
            atomic_write(
                path,
                base64.b64decode(saved["data"]),
                saved["mode"],
                saved["uid"],
                saved["gid"],
            )
        del self.data["policy"]
        self.save()

    @contextlib.contextmanager
    def _no_service_start(self):
        path = regular(self.path("/usr/sbin/policy-rc.d"))
        saved = {"data": None}
        if path.exists():
            info = path.stat()
            saved = {
                "data": base64.b64encode(path.read_bytes()).decode(),
                "mode": info.st_mode & 0o7777,
                "uid": info.st_uid,
                "gid": info.st_gid,
            }
        self.data["policy"] = saved
        self.save()
        atomic_write(path, POLICY, 0o755, 0, 0)
        try:
            yield
        finally:
            self._recover_policy()

    def install_packages(self, packages):
        installed = self.installed()
        missing = [
            name
            for name in packages
            if name not in {p.split(":", 1)[0] for p in installed}
        ]
        if not missing:
            return
        self.run("apt-get", "update")
        options = ("--no-install-recommends", "--no-upgrade", "--no-remove")
        simulation = self.run(
            "apt-get", "-s", "install", *options, *missing, capture_output=True
        ).stdout
        candidates = [
            line.split()[1]
            for line in simulation.splitlines()
            if line.startswith("Inst ")
        ]
        self.data["pending"] = {
            "before": sorted(self.present()),
            "candidates": candidates,
            "units": {name: self._unit(name) for name in SERVICES},
        }
        self.save()
        try:
            with self._no_service_start():
                self.run(
                    "apt-get",
                    "install",
                    "-y",
                    *options,
                    *missing,
                )
        finally:
            self._finish_install()

    def _finish_install(self):
        pending = self.data["pending"]
        self._track(pending["before"], pending["candidates"])
        self.run("systemctl", "daemon-reload")
        # Debian postinst may enable a new unit even when policy-rc.d blocks start.
        for name, state in pending["units"].items():
            now = self._unit(name)
            if (
                state.get("LoadState") == "not-found"
                and now.get("LoadState") == "loaded"
            ):
                if now.get("ActiveState") == "active":
                    raise ValueError(
                        f"Package unexpectedly started {name}; dependency records retained"
                    )
                if now.get("UnitFileState") in {"enabled", "enabled-runtime"}:
                    self.run("systemctl", "disable", name)
        del self.data["pending"]
        self.save()

    def _prepare_dns(self):
        if "resolvconf" in {p.split(":", 1)[0] for p in self.installed()}:
            return
        if "resolver" not in self.data:
            self._save_dns()
        # Track the resolver link ourselves instead of letting postinst replace it.
        self.run(
            "debconf-set-selections",
            input="resolvconf resolvconf/linkify-resolvconf boolean false\n",
        )

    def _save_dns(self):
        path = self.path("/etc/resolv.conf")
        original = {
            "link": os.readlink(path) if path.is_symlink() else None,
            "data": None,
            "mode": 0o644,
            "uid": 0,
            "gid": 0,
        }
        if path.exists():
            original["data"] = base64.b64encode(path.read_bytes()).decode()
            original["mode"] = path.stat().st_mode & 0o7777
            original["uid"] = path.stat().st_uid
            original["gid"] = path.stat().st_gid
        self.data["resolver"] = original
        self.save()

    def _configure_dns(self):
        saved = self.data.get("resolver")
        if saved is None:
            return
        text = base64.b64decode(saved["data"]).decode() if saved["data"] else ""
        self.run("resolvconf", "--enable-updates")
        self.run("resolvconf", "-a", "forcad-bootstrap", input=text)
        path = self.path("/etc/resolv.conf")
        if path.is_symlink() and os.readlink(path) in {
            "/run/resolvconf/resolv.conf",
            "/var/run/resolvconf/resolv.conf",
            "../run/resolvconf/resolv.conf",
        }:
            return
        if path.is_symlink() != bool(saved["link"]) or (
            path.is_symlink() and os.readlink(path) != saved["link"]
        ):
            raise ValueError(
                "Resolver configuration changed during dependency installation"
            )
        if (
            path.exists()
            and not path.is_symlink()
            and path.read_bytes() != base64.b64decode(saved["data"] or "")
        ):
            raise ValueError(
                "Resolver configuration changed during dependency installation"
            )
        path.unlink(missing_ok=True)
        path.symlink_to("/run/resolvconf/resolv.conf")

    def ensure_sensor(self):
        group = self.run(
            "getent", "group", "forcad-sensor", check=False, capture_output=True
        )
        if group.returncode not in {0, 2}:
            raise ValueError("Could not inspect forcad-sensor group")
        if group.returncode == 2:
            self.run("groupadd", "--system", "forcad-sensor")
            group = self.run("getent", "group", "forcad-sensor", capture_output=True)
            self.data["accounts"]["sensor_group"] = int(group.stdout.split(":")[2])
            self.save()
        user = self.run(
            "getent", "passwd", "forcad-sensor", check=False, capture_output=True
        )
        if user.returncode not in {0, 2}:
            raise ValueError("Could not inspect forcad-sensor account")
        if user.returncode == 2:
            self.run(
                "useradd",
                "--system",
                "--no-create-home",
                "--home-dir",
                "/nonexistent",
                "--shell",
                "/usr/sbin/nologin",
                "--gid",
                "forcad-sensor",
                "forcad-sensor",
            )
            user = self.run("getent", "passwd", "forcad-sensor", capture_output=True)
            self.data["accounts"]["sensor_user"] = int(user.stdout.split(":")[2])
            self.save()
        fields = user.stdout.strip().split(":")
        gid = int(group.stdout.split(":")[2])
        if not (
            0 < int(fields[2]) < 1000
            and 0 < gid < 1000
            and int(fields[3]) == gid
            and fields[-1].endswith("/nologin")
        ):
            raise ValueError(
                "Existing forcad-sensor must be a system account with a nologin shell"
            )

    def install(self, game_metrics=False):
        distribution = {}
        for line in self.path("/etc/os-release").read_text().splitlines():
            key, separator, value = line.partition("=")
            if separator:
                distribution[key] = value.strip('"')
        if distribution.get("ID") != "debian" or distribution.get("VERSION_ID") not in {
            "12",
            "13",
        }:
            raise ValueError("Automatic dependencies require Debian 12 or 13")
        self._open()
        self.data["distribution"] = distribution["VERSION_ID"]
        self.save()
        self._prepare_dns()
        packages = [
            *BASE,
            *(["vlan"] if self.role == "router" else ["wireguard-tools"]),
        ]
        self.install_packages(packages)
        if not self.bundle.files_only:
            self._configure_dns()
        if game_metrics:
            from .zeek import ensure_zeek

            ensure_zeek(self)
            self.ensure_sensor()
        print("PASS host dependencies installed")

    def _restore_dns(self):
        saved = self.data.get("resolver")
        if saved is None:
            return
        path = self.path("/etc/resolv.conf")
        # Only replace the link created by this bootstrap, or an already restored file.
        if path.is_symlink() and os.readlink(path) not in {
            "/run/resolvconf/resolv.conf",
            "/var/run/resolvconf/resolv.conf",
            "../run/resolvconf/resolv.conf",
            saved["link"],
        }:
            raise ValueError("Resolver link has changed; dependency state retained")
        if (
            not path.is_symlink()
            and path.exists()
            and path.read_bytes() != base64.b64decode(saved["data"] or "")
        ):
            raise ValueError("Resolver file has changed; dependency state retained")
        if shutil.which("resolvconf"):
            self.run("resolvconf", "-d", "forcad-bootstrap")
        if saved["link"] and path.is_symlink() and os.readlink(path) == saved["link"]:
            return
        if not saved["link"] and not path.is_symlink():
            if saved["data"] is None and not path.exists():
                return
            if path.exists() and path.read_bytes() == base64.b64decode(
                saved["data"] or ""
            ):
                info = path.stat()
                if (info.st_mode & 0o7777, info.st_uid, info.st_gid) == (
                    saved["mode"],
                    saved.get("uid", 0),
                    saved.get("gid", 0),
                ):
                    return
        path.unlink(missing_ok=True)
        if saved["link"]:
            path.symlink_to(saved["link"])
        elif saved["data"] is not None:
            atomic_write(
                path,
                base64.b64decode(saved["data"]),
                saved["mode"],
                saved.get("uid", 0),
                saved.get("gid", 0),
            )

    def _remove_accounts(self, purge):
        names = [
            ("sensor_user", "passwd", "forcad-sensor", "userdel"),
            ("sensor_group", "group", "forcad-sensor", "groupdel"),
        ]
        if purge:
            names.append(("zeek_group", "group", "zeek", "groupdel"))
        for key, database, name, tool in names:
            identity = self.data["accounts"].get(key)
            if identity is None:
                continue
            current = self.run(
                "getent", database, name, capture_output=True, check=False
            )
            if current.returncode not in {0, 2}:
                raise ValueError(f"Could not inspect account {name}")
            if current.returncode == 0:
                if int(current.stdout.split(":")[2]) != identity:
                    raise ValueError(f"Account {name} changed; refusing removal")
                if database == "group" and current.stdout.strip().split(":")[3]:
                    raise ValueError(
                        f"Group {name} has additional members; refusing removal"
                    )
                self.run(tool, name)
            self.data["accounts"].pop(key)
            self.save()

    def uninstall(self, choice=None, dry_run=False):
        if not self._read():
            return
        if dry_run:
            print(
                "Would offer removal of recorded dependency packages after host restoration."
            )
            return
        self._open()
        packages = sorted(set(self.data["packages"]) & self.present())
        self.data["packages"] = packages
        purge = choice == "purge"
        if choice is None and any(
            self.data[key] for key in ("packages", "files", "directories", "accounts")
        ):
            print("Packages installed by this installer: " + ", ".join(packages))
            try:
                purge = input(
                    "Remove these packages and their install-owned data? [y/N] "
                ).strip().lower() in {"y", "yes"}
            except EOFError:
                purge = False
        self._remove_accounts(purge=False)
        # resolvconf postrm leaves the already restored resolver path alone.
        self._restore_dns()
        if purge and packages:
            simulation = self.run(
                "apt-get", "-s", "purge", "--", *packages, capture_output=True
            ).stdout
            native = self.run(
                "dpkg", "--print-architecture", capture_output=True
            ).stdout.strip()

            def qualified(name):
                return name if ":" in name else f"{name}:{native}"

            allowed = {qualified(name) for name in packages}
            for line in simulation.splitlines():
                fields = line.split()
                if (
                    fields
                    and fields[0] in {"Remv", "Purg"}
                    and qualified(fields[1]) not in allowed
                ):
                    raise ValueError(
                        f"Package purge would remove unowned package: {fields[1]}"
                    )
            self.run("apt-get", "purge", "-y", "--", *packages)
        self._restore_dns()
        if purge:
            self._remove_accounts(purge=True)
            for name, digest in list(self.data["files"].items()):
                path = regular(self.path(name))
                if (
                    path.exists()
                    and hashlib.sha256(path.read_bytes()).hexdigest() != digest
                ):
                    raise ValueError(f"Dependency file changed: {name}")
                path.unlink(missing_ok=True)
                del self.data["files"][name]
                self.save()
            for name in sorted(
                set(self.data["directories"]),
                key=lambda value: len(Path(value).parts),
                reverse=True,
            ):
                path = self.path(name)
                if path.is_symlink():
                    raise ValueError(f"Dependency directory changed: {name}")
                if path.is_dir() and not any(path.iterdir()):
                    path.rmdir()
        else:
            self.data.pop("resolver", None)
            self.save()
            if any(
                self.data[key]
                for key in ("packages", "files", "directories", "accounts")
            ):
                print("Dependency packages kept; ownership retained for later cleanup")
                return
        for name in ("state.json", "owner"):
            (self.directory / name).unlink(missing_ok=True)
        self.directory.rmdir()
        if not any(self.directory.parent.iterdir()):
            self.directory.parent.rmdir()
        print(
            "PASS dependency packages removed" if purge else "Dependency packages kept"
        )

import hashlib
import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from helpers import ROOT

if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from hostlib.files import atomic_write as real_atomic_write
from hostlib.packages import POLICY, Dependencies


class Bundle:
    def __init__(self, role, root):
        self.role = role
        self.root = Path(root)
        self.files_only = True

    def path(self, absolute):
        return self.root / absolute.lstrip("/")


class DependencyTests(unittest.TestCase):
    def setUp(self):
        scratch = tempfile.TemporaryDirectory()
        self.addCleanup(scratch.cleanup)
        self.work = Path(scratch.name)
        self.root = self.work / "host"
        self.root.mkdir()
        self.path("/etc/os-release").parent.mkdir(parents=True)
        self.path("/etc/os-release").write_text('ID=debian\nVERSION_ID="12"\n')
        self.calls = []
        self.installed_packages = set()
        self.package_statuses = {}
        self.accounts = {"group": {}, "passwd": {}}
        self.package_install_failure = False
        self.partial_packages = 1
        self.partial_status = "half-configured"
        self.purge_simulation = ""
        self.install_simulation = None
        self.last_candidates = set()
        self.addition_on_install = {"libdependency:amd64"}
        self.policy_during_install = []
        self.service_states_before = {}
        self.service_states_after = {}
        self.package_install_started = False
        patcher = patch("hostlib.packages.atomic_write", side_effect=self._atomic_write)
        patcher.start()
        self.addCleanup(patcher.stop)

    def path(self, absolute):
        return self.root / absolute.lstrip("/")

    def _atomic_write(self, path, data, mode, _uid, _gid):
        real_atomic_write(path, data, mode, os.geteuid(), os.getegid())

    def manager(self, role="vpn"):
        manager = Dependencies(Bundle(role, self.root))
        manager.installed = lambda: set(self.installed_packages)
        manager.present = lambda: (
            set(self.installed_packages)
            | {
                name
                for name, status in self.package_statuses.items()
                if status != "not-installed"
            }
        )
        manager.run = self.fake_run
        return manager

    @staticmethod
    def _qualified(package):
        return package if ":" in package else f"{package}:amd64"

    def _simulation(self, command):
        if self.install_simulation is not None:
            output = self.install_simulation
            self.last_candidates = {
                line.split()[1]
                for line in output.splitlines()
                if line.startswith("Inst ")
            }
            self.last_candidates = {
                self._qualified(name) for name in self.last_candidates
            }
            return output

        requested = {
            argument for argument in command[3:] if not argument.startswith("-")
        }
        names = {self._qualified(name) for name in requested}
        names.update(self._qualified(name) for name in self.addition_on_install)
        self.last_candidates = names
        return "".join(
            f"Inst {name} (1.0 Debian:12 [amd64])\n" for name in sorted(names)
        )

    def fake_run(self, *args, **kwargs):
        command = tuple(str(argument) for argument in args)
        self.calls.append((command, kwargs.copy()))

        if command[:2] == ("systemctl", "show"):
            default = {
                "LoadState": "not-found",
                "ActiveState": "inactive",
                "UnitFileState": "disabled",
            }
            states = (
                self.service_states_after
                if self.package_install_started
                else self.service_states_before
            )
            service = states.get(command[2], default)
            return subprocess.CompletedProcess(
                command,
                0,
                stdout="".join(f"{key}={value}\n" for key, value in service.items()),
            )
        if command == ("dpkg", "--print-architecture"):
            return subprocess.CompletedProcess(command, 0, stdout="amd64\n")
        if command[:3] == ("apt-get", "-s", "purge"):
            return subprocess.CompletedProcess(command, 0, stdout=self.purge_simulation)
        if command[:3] == ("apt-get", "-s", "install"):
            return subprocess.CompletedProcess(
                command, 0, stdout=self._simulation(command)
            )
        if command[:2] == ("apt-get", "install"):
            self.package_install_started = True
            policy = self.path("/usr/sbin/policy-rc.d")
            self.policy_during_install.append(
                policy.read_bytes() if policy.exists() else None
            )
            packages = sorted(self.last_candidates)
            if self.package_install_failure:
                for package in packages[: self.partial_packages]:
                    self.package_statuses[package] = self.partial_status
                raise subprocess.CalledProcessError(1, command)
            self.installed_packages.update(packages)
            for package in packages:
                self.package_statuses[package] = "installed"
            self.installed_packages.update(self.addition_on_install)
            for package in self.addition_on_install:
                self.package_statuses[package] = "installed"
        elif command[:2] == ("apt-get", "purge"):
            packages = list(command[command.index("--") + 1 :])
            self.installed_packages.difference_update(packages)
            for package in packages:
                self.package_statuses.pop(package, None)
        elif command[:2] == ("systemctl", "disable"):
            unit = command[2]
            self.service_states_after.setdefault(
                unit,
                {
                    "LoadState": "loaded",
                    "ActiveState": "inactive",
                    "UnitFileState": "disabled",
                },
            )["UnitFileState"] = "disabled"
        elif command[0] == "getent":
            database, name = command[1:3]
            entry = self.accounts[database].get(name)
            return subprocess.CompletedProcess(
                command,
                0 if entry else 2,
                stdout=f"{entry}\n" if entry else "",
            )
        elif command[0] == "groupadd":
            self.accounts["group"][command[-1]] = f"{command[-1]}:x:901:"
        elif command[0] == "useradd":
            self.accounts["passwd"][command[-1]] = (
                f"{command[-1]}:x:902:901::/nonexistent:/usr/sbin/nologin"
            )
        elif command[0] == "userdel":
            self.accounts["passwd"].pop(command[-1], None)
        elif command[0] == "groupdel":
            self.accounts["group"].pop(command[-1], None)
        elif command[0] == "resolvconf" and command[1:2] == ("-a",):
            resolver = self.path("/etc/resolv.conf")
            resolver.unlink(missing_ok=True)
            resolver.symlink_to("/run/resolvconf/resolv.conf")

        return subprocess.CompletedProcess(command, 0, stdout="")

    def create_state(
        self, manager, packages=(), files=None, directories=(), accounts=None
    ):
        manager._open()
        manager.data["packages"] = sorted(packages)
        manager.data["files"] = files or {}
        manager.data["directories"] = list(directories)
        manager.data["accounts"] = accounts or {}
        manager.save()

    def state(self, role="vpn"):
        return self.path(f"/var/lib/forcad-dependencies/{role}/state.json")

    def snapshot(self):
        result = {}
        for path in sorted(self.root.rglob("*")):
            name = str(path.relative_to(self.root))
            if path.is_symlink():
                result[name] = ("link", os.readlink(path))
            elif path.is_dir():
                result[name] = ("dir", stat.S_IMODE(path.stat().st_mode))
            elif path.is_file():
                result[name] = (
                    "file",
                    path.read_bytes(),
                    stat.S_IMODE(path.stat().st_mode),
                )
        return result

    def test_preexisting_packages_are_not_owned_and_later_additions_are_ignored(self):
        self.installed_packages.update({"python3:amd64", "nftables:amd64"})
        manager = self.manager()

        manager.install()

        owned = json.loads(self.state().read_text())["packages"]
        self.assertNotIn("python3:amd64", owned)
        self.assertNotIn("nftables:amd64", owned)
        self.assertIn("iproute2:amd64", owned)
        self.assertIn("libdependency:amd64", owned)

        self.installed_packages.add("operator-tool")
        manager.install_packages(["python3", "nftables", "iproute2"])
        self.assertNotIn(
            "operator-tool", json.loads(self.state().read_text())["packages"]
        )

    def test_status_queries_include_residual_and_partial_dpkg_states(self):
        manager = Dependencies(Bundle("vpn", self.root))
        output = """\
installed bash
config-files old-package:amd64
unpacked unpacked-package:amd64
half-configured broken-package:amd64
not-installed absent-package:amd64
"""
        manager.run = lambda *args, **kwargs: subprocess.CompletedProcess(
            args, 0, stdout=output
        )

        self.assertEqual(manager.installed(), {"bash"})
        self.assertEqual(
            manager.present(),
            {
                "bash",
                "old-package:amd64",
                "unpacked-package:amd64",
                "broken-package:amd64",
            },
        )

    def test_partial_package_status_is_owned_from_the_apt_plan(self):
        manager = self.manager()
        self.create_state(manager)
        self.package_install_failure = True
        self.partial_status = "unpacked"
        self.addition_on_install.clear()

        with self.assertRaises(subprocess.CalledProcessError):
            manager.install_packages(["partial-package"])

        state = json.loads(self.state().read_text())
        self.assertIn("partial-package:amd64", state["packages"])
        self.assertNotIn("pending", state)

    def test_preexisting_config_files_package_is_not_claimed_after_reinstall(self):
        self.package_statuses["old-package:amd64"] = "config-files"
        self.addition_on_install.clear()
        manager = self.manager()
        self.create_state(manager)

        manager.install_packages(["old-package"])

        state = json.loads(self.state().read_text())
        self.assertNotIn("old-package:amd64", state["packages"])
        self.assertIn("old-package:amd64", self.installed_packages)

    def test_pending_recovery_tracks_only_planned_additions_and_disables_new_unit(self):
        self.installed_packages.update(
            {"new-package:amd64", "operator-package:amd64", "base-package:amd64"}
        )
        self.package_statuses.update(
            {
                "new-package:amd64": "installed",
                "operator-package:amd64": "installed",
                "base-package:amd64": "installed",
            }
        )
        self.package_install_started = True
        self.service_states_after["nftables.service"] = {
            "LoadState": "loaded",
            "ActiveState": "inactive",
            "UnitFileState": "enabled",
        }
        manager = self.manager()
        self.create_state(manager)
        manager.data["pending"] = {
            "before": ["base-package:amd64"],
            "candidates": ["new-package"],
            "units": {
                "nftables.service": {
                    "LoadState": "not-found",
                    "ActiveState": "inactive",
                    "UnitFileState": "disabled",
                }
            },
        }
        manager.save()

        retry = self.manager()
        retry._open()

        state = json.loads(self.state().read_text())
        self.assertEqual(state["packages"], ["new-package:amd64"])
        self.assertNotIn("pending", state)
        self.assertIn(
            ("systemctl", "disable", "nftables.service"),
            [command for command, _ in self.calls],
        )

    def test_empty_package_receipt_prompts_and_purges_owned_repository_file(self):
        name = "/etc/apt/sources.list.d/forcad-zeek.list"
        content = b"deb [arch=amd64] https://example.invalid/zeek/ /\n"
        path = self.path(name)
        path.parent.mkdir(parents=True)
        path.write_bytes(content)
        manager = self.manager()
        self.create_state(
            manager,
            files={name: hashlib.sha256(content).hexdigest()},
            directories=["/etc/apt/sources.list.d"],
        )

        with patch("builtins.input", return_value="yes") as prompt:
            manager.uninstall()

        prompt.assert_called_once()
        self.assertFalse(path.exists())
        self.assertFalse(manager.directory.exists())
        self.assertFalse(
            any(command[:2] == ("apt-get", "purge") for command, _ in self.calls)
        )

    def test_keep_then_reinstall_reuses_owned_repository_file(self):
        name = "/etc/apt/sources.list.d/forcad-zeek.list"
        content = b"deb [arch=amd64] https://example.invalid/zeek/ /\n"
        path = self.path(name)
        path.parent.mkdir(parents=True)
        path.write_bytes(content)
        self.installed_packages.add("curl:amd64")
        manager = self.manager()
        self.create_state(
            manager,
            packages={"curl:amd64"},
            files={name: hashlib.sha256(content).hexdigest()},
            directories=["/etc/apt/sources.list.d"],
        )

        manager.uninstall(choice="keep")

        retry = self.manager()
        retry._open()
        retry.own_file(name, content)

        self.assertTrue(retry.directory.exists())
        self.assertEqual(path.read_bytes(), content)
        self.assertEqual(
            json.loads(self.state().read_text())["files"][name],
            hashlib.sha256(content).hexdigest(),
        )

    def test_failed_apt_install_restores_policy_and_retains_retry_state_without_manifest(
        self,
    ):
        policy = self.path("/usr/sbin/policy-rc.d")
        policy.parent.mkdir(parents=True)
        policy.write_bytes(b"#!/bin/sh\nexit 0\n")
        policy.chmod(0o750)
        self.installed_packages.add("python3:amd64")
        self.package_install_failure = True
        manager = self.manager()

        with self.assertRaises(subprocess.CalledProcessError):
            manager.install()

        self.assertEqual(policy.read_bytes(), b"#!/bin/sh\nexit 0\n")
        self.assertEqual(stat.S_IMODE(policy.stat().st_mode), 0o750)
        failed_state = json.loads(self.state().read_text())
        self.assertIn("ifupdown:amd64", failed_state["packages"])
        self.assertNotIn("pending", failed_state)
        self.assertNotIn("policy", failed_state)
        self.assertFalse(self.path("/etc/forcad/vpn/install.json").exists())
        self.assertEqual(self.policy_during_install, [POLICY])

        self.package_install_failure = False
        self.addition_on_install.clear()
        manager.install()

        retried_state = json.loads(self.state().read_text())
        self.assertIn("ifupdown:amd64", retried_state["packages"])
        self.assertIn("wireguard-tools:amd64", retried_state["packages"])
        self.assertFalse(self.path("/etc/forcad/vpn/install.json").exists())
        self.assertEqual(policy.read_bytes(), b"#!/bin/sh\nexit 0\n")
        self.assertEqual(self.policy_during_install, [POLICY, POLICY])
        installs = [
            call for call, _ in self.calls if call[:2] == ("apt-get", "install")
        ]
        self.assertEqual(len(installs), 2)

    def test_new_service_is_disabled_without_being_started(self):
        manager = self.manager()
        self.create_state(manager)
        self.service_states_before["nftables.service"] = {
            "LoadState": "not-found",
            "ActiveState": "inactive",
            "UnitFileState": "disabled",
        }
        self.service_states_after["nftables.service"] = {
            "LoadState": "loaded",
            "ActiveState": "inactive",
            "UnitFileState": "enabled",
        }

        manager.install_packages(["nftables"])

        commands = [command for command, _ in self.calls]
        self.assertIn(("systemctl", "disable", "nftables.service"), commands)
        self.assertFalse(
            any(
                command[0] == "systemctl"
                and len(command) > 1
                and command[1] in {"start", "restart", "reload"}
                for command in commands
            )
        )
        self.assertEqual(self.policy_during_install, [POLICY])

    def test_newly_started_service_fails_and_keeps_package_ownership_record(self):
        manager = self.manager()
        self.create_state(manager)
        self.service_states_before["nftables.service"] = {
            "LoadState": "not-found",
            "ActiveState": "inactive",
            "UnitFileState": "disabled",
        }
        self.service_states_after["nftables.service"] = {
            "LoadState": "loaded",
            "ActiveState": "active",
            "UnitFileState": "enabled",
        }

        with self.assertRaisesRegex(
            ValueError, "Package unexpectedly started nftables.service"
        ):
            manager.install_packages(["nftables"])

        self.assertIn(
            "nftables:amd64", json.loads(self.state().read_text())["packages"]
        )
        self.assertFalse(self.path("/usr/sbin/policy-rc.d").exists())
        self.assertFalse(
            any(
                command[0] == "systemctl"
                and len(command) > 1
                and command[1] in {"start", "restart", "reload"}
                for command, _ in self.calls
            )
        )

    def test_dry_run_does_not_mutate_state_or_prompt(self):
        manager = self.manager()
        self.installed_packages.add("owned-package")
        self.create_state(manager, packages={"owned-package"})
        before = self.snapshot()

        with patch(
            "builtins.input", side_effect=AssertionError("unexpected prompt")
        ) as prompt:
            manager.uninstall(dry_run=True)

        self.assertEqual(self.snapshot(), before)
        prompt.assert_not_called()
        self.assertEqual(self.calls, [])

    def test_keep_choice_retains_receipt_and_keeps_packages(self):
        manager = self.manager()
        self.installed_packages.update({"owned-package", "preexisting-package"})
        self.create_state(manager, packages={"owned-package"})

        manager.uninstall(choice="keep")

        self.assertEqual(
            self.installed_packages, {"owned-package", "preexisting-package"}
        )
        self.assertTrue(manager.directory.exists())
        self.assertEqual(
            json.loads(self.state().read_text())["packages"], ["owned-package"]
        )
        self.assertFalse(
            any(call[0][:2] == ("apt-get", "purge") for call in self.calls)
        )

    def test_no_added_dependencies_leave_no_receipt_or_prompt(self):
        for choice in (None, "keep"):
            with self.subTest(choice=choice):
                manager = self.manager()
                self.create_state(manager)
                with patch("builtins.input") as prompt:
                    manager.uninstall(choice=choice)
                prompt.assert_not_called()
                self.assertFalse(manager.directory.exists())
                self.assertFalse(manager.directory.parent.exists())
        self.assertFalse(
            any(command[:2] == ("apt-get", "purge") for command, _ in self.calls)
        )

    def test_already_removed_packages_leave_no_receipt_or_prompt(self):
        manager = self.manager()
        self.create_state(manager, packages={"old-package"})
        with patch("builtins.input") as prompt:
            manager.uninstall()
        prompt.assert_not_called()
        self.assertFalse(manager.directory.exists())
        self.assertFalse(
            any(command[:2] == ("apt-get", "purge") for command, _ in self.calls)
        )

    def test_purge_choice_removes_recorded_packages_and_keeps_unowned_ones(self):
        manager = self.manager()
        self.installed_packages.update({"owned-package", "preexisting-package"})
        self.create_state(manager, packages={"owned-package"})
        self.purge_simulation = "Remv owned-package [1.0]\n"

        manager.uninstall(choice="purge")

        self.assertEqual(self.installed_packages, {"preexisting-package"})
        self.assertFalse(manager.directory.exists())
        self.assertTrue(
            any(
                call[0] == ("apt-get", "purge", "-y", "--", "owned-package")
                for call in self.calls
            )
        )

    def test_purge_rejects_a_cascade_into_an_unowned_package(self):
        manager = self.manager()
        self.installed_packages.update({"owned-package", "preexisting-package"})
        self.create_state(manager, packages={"owned-package"})
        self.purge_simulation = (
            "Remv owned-package [1.0]\nRemv preexisting-package [1.0]\n"
        )

        with self.assertRaisesRegex(ValueError, "unowned package: preexisting-package"):
            manager.uninstall(choice="purge")

        self.assertIn("owned-package", self.installed_packages)
        self.assertIn("preexisting-package", self.installed_packages)
        self.assertTrue(manager.directory.exists())
        self.assertFalse(
            any(call[0][:2] == ("apt-get", "purge") for call in self.calls)
        )

    def test_purge_rejects_a_cascade_into_an_unowned_architecture(self):
        manager = self.manager()
        self.installed_packages.add("owned-package:amd64")
        self.create_state(manager, packages={"owned-package:amd64"})
        self.purge_simulation = "Remv owned-package:i386 [1.0]\n"

        with self.assertRaisesRegex(ValueError, "unowned package: owned-package:i386"):
            manager.uninstall(choice="purge")

        self.assertIn("owned-package:amd64", self.installed_packages)
        self.assertTrue(manager.directory.exists())
        self.assertFalse(
            any(call[0][:2] == ("apt-get", "purge") for call in self.calls)
        )

    def test_sensor_accounts_are_removed_only_when_recorded_ids_match(self):
        manager = self.manager()
        self.create_state(manager)

        manager.ensure_sensor()

        self.assertEqual(
            manager.data["accounts"], {"sensor_group": 901, "sensor_user": 902}
        )
        self.accounts["passwd"]["forcad-sensor"] = (
            "forcad-sensor:x:903:901::/nonexistent:/usr/sbin/nologin"
        )
        with self.assertRaisesRegex(ValueError, "Account forcad-sensor changed"):
            manager._remove_accounts(purge=False)
        self.assertFalse(any(call[0][0] == "userdel" for call in self.calls))

        self.accounts["passwd"]["forcad-sensor"] = (
            "forcad-sensor:x:902:901::/nonexistent:/usr/sbin/nologin"
        )
        manager._remove_accounts(purge=False)
        self.assertNotIn("forcad-sensor", self.accounts["passwd"])
        self.assertNotIn("forcad-sensor", self.accounts["group"])
        self.assertIn(("userdel", "forcad-sensor"), [call for call, _ in self.calls])
        self.assertIn(("groupdel", "forcad-sensor"), [call for call, _ in self.calls])

    def test_preexisting_sensor_accounts_are_never_recorded_or_removed(self):
        manager = self.manager()
        self.accounts["group"]["forcad-sensor"] = "forcad-sensor:x:901:"
        self.accounts["passwd"]["forcad-sensor"] = (
            "forcad-sensor:x:902:901::/nonexistent:/usr/sbin/nologin"
        )
        self.create_state(manager)

        manager.ensure_sensor()
        manager._remove_accounts(purge=True)

        self.assertEqual(manager.data["accounts"], {})
        self.assertIn("forcad-sensor", self.accounts["passwd"])
        self.assertIn("forcad-sensor", self.accounts["group"])
        self.assertFalse(
            any(call[0][0] in {"userdel", "groupdel"} for call in self.calls)
        )

    def test_dns_file_and_mode_are_restored_after_resolvconf_replacement(self):
        resolver = self.path("/etc/resolv.conf")
        resolver.parent.mkdir(parents=True, exist_ok=True)
        resolver.write_text("nameserver 192.0.2.53\nsearch example.test\n")
        resolver.chmod(0o640)
        manager = self.manager(role="router")
        self.create_state(manager)

        manager._prepare_dns()
        with patch("hostlib.packages.shutil.which", return_value="resolvconf"):
            manager._configure_dns()
            self.assertEqual(os.readlink(resolver), "/run/resolvconf/resolv.conf")
            manager._restore_dns()

        self.assertTrue(resolver.is_file())
        self.assertEqual(
            resolver.read_text(), "nameserver 192.0.2.53\nsearch example.test\n"
        )
        self.assertEqual(stat.S_IMODE(resolver.stat().st_mode), 0o640)

    def test_original_dns_symlink_is_restored(self):
        resolver = self.path("/etc/resolv.conf")
        resolver.parent.mkdir(parents=True, exist_ok=True)
        resolver.symlink_to("/run/systemd/resolve/stub-resolv.conf")
        manager = self.manager(role="router")
        self.create_state(manager)

        manager._prepare_dns()
        with patch("hostlib.packages.shutil.which", return_value="resolvconf"):
            manager._configure_dns()
            self.assertEqual(os.readlink(resolver), "/run/resolvconf/resolv.conf")
            manager._restore_dns()

        self.assertTrue(resolver.is_symlink())
        self.assertEqual(os.readlink(resolver), "/run/systemd/resolve/stub-resolv.conf")


if __name__ == "__main__":
    unittest.main()

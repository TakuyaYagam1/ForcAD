"""Exercise Zeek package setup with a fake host dependency manager."""

import hashlib
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import helpers  # noqa: F401 - add infrastructure modules to sys.path

from hostlib import zeek

TEST_KEY = b"-----BEGIN PGP PUBLIC KEY BLOCK-----\nfixture\n"


class FakeDependencies:
    def __init__(self, distribution="13", architecture="amd64", group_gid=None):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.data = {
            "distribution": distribution,
            "directories": [],
            "accounts": {},
            "packages": [],
        }
        self.architecture = architecture
        self.group_gid = group_gid
        self.calls = []
        self.install_calls = []
        self.files = {}
        self.key = TEST_KEY
        self.versions = {}
        self.present_packages = set()
        self.installed_packages = set()

    def close(self):
        self.temp.cleanup()

    def path(self, absolute):
        return self.root / absolute.lstrip("/")

    def run(self, *args, **kwargs):
        self.calls.append(("run", args, kwargs))
        if args == ("dpkg", "--print-architecture"):
            return subprocess.CompletedProcess(args, 0, self.architecture + "\n", "")
        if args == ("getent", "group", "zeek"):
            if self.group_gid is None:
                return subprocess.CompletedProcess(args, 2, "", "")
            return subprocess.CompletedProcess(
                args, 0, f"zeek:x:{self.group_gid}:\n", ""
            )
        if args[0] == "curl":
            return subprocess.CompletedProcess(args, 0, self.key, b"")
        if args[-1:] == ("--version",):
            return subprocess.CompletedProcess(args, 0, self.versions[str(args[0])], "")
        if args == ("apt-get", "update"):
            return subprocess.CompletedProcess(args, 0, "", "")
        raise AssertionError(args)

    def install_packages(self, packages):
        packages = list(packages)
        self.calls.append(("install_packages", packages))
        self.install_calls.append(packages)
        self.run("apt-get", "update")
        if "zeek-9.0-core" in packages:
            self.present_packages.add("zeek-9.0-core:amd64")
            self.installed_packages.add("zeek-9.0-core:amd64")
            binary = self.path("/opt/zeek/bin/zeek")
            binary.parent.mkdir(parents=True, exist_ok=True)
            binary.write_bytes(b"fake Zeek executable")
            self.versions[str(binary)] = "zeek version 9.0.0\n"
            if self.group_gid is None:
                self.group_gid = 988

    def present(self):
        return set(self.present_packages)

    def installed(self):
        return set(self.installed_packages)

    def own_file(self, path, data, mode=0o644):
        self.calls.append(("own_file", path, data, mode))
        target = self.path(path)
        if target.exists() and path not in self.files:
            raise ValueError(f"Refusing unowned file: {path}")
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        target.chmod(mode)
        self.files[path] = data

    def save(self):
        self.calls.append(("save",))


class ZeekTests(unittest.TestCase):
    def setUp(self):
        self.deps = FakeDependencies()
        self.addCleanup(self.deps.close)

    def ensure_with_test_key(self, deps=None):
        deps = deps or self.deps
        key_hash = hashlib.sha256(deps.key).hexdigest()
        with (
            patch.object(zeek, "KEY_SHA256", key_hash),
            self.root_owned_runtime(deps),
        ):
            return zeek.ensure_zeek(deps)

    def root_owned_runtime(self, deps=None, overrides=None):
        deps = deps or self.deps
        overrides = overrides or {}
        protected = {
            deps.path("/opt"),
            deps.path("/opt/zeek"),
            deps.path("/opt/zeek/bin"),
            deps.path("/opt/zeek/bin/zeek"),
        }
        original_stat = Path.stat

        def fake_stat(path, *args, **kwargs):
            info = original_stat(path, *args, **kwargs)
            if path not in protected:
                return info
            changes = overrides.get(path, {})
            return SimpleNamespace(
                st_mode=changes.get("st_mode", info.st_mode),
                st_uid=changes.get("st_uid", 0),
            )

        return patch.object(Path, "stat", fake_stat)

    def test_reuses_an_existing_zeek_9_without_package_changes(self):
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing runtime")
        self.deps.versions[str(binary)] = "zeek version 9.2.1\n"

        version = self.ensure_with_test_key()

        self.assertEqual(version, "9.2.1")
        self.assertEqual(self.deps.install_calls, [])
        self.assertEqual(self.deps.files, {})
        self.assertNotIn("/opt/zeek", self.deps.data["directories"])

    def test_rejects_an_existing_old_zeek_without_attempting_upgrade(self):
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing runtime")
        self.deps.versions[str(binary)] = "zeek version 8.2.2\n"

        with self.assertRaisesRegex(ValueError, "Zeek 9 or newer"):
            self.ensure_with_test_key()

        self.assertEqual(self.deps.install_calls, [])
        self.assertEqual(self.deps.files, {})

    def test_rejects_an_untrusted_existing_runtime_before_execution(self):
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing runtime")
        self.deps.versions[str(binary)] = "zeek version 9.2.1\n"

        with (
            patch.object(zeek, "KEY_SHA256", hashlib.sha256(TEST_KEY).hexdigest()),
            self.root_owned_runtime(
                overrides={binary: {"st_mode": binary.stat().st_mode | stat.S_IWGRP}}
            ),
            self.assertRaisesRegex(ValueError, "group/world-writable"),
        ):
            zeek.ensure_zeek(self.deps)

        self.assertEqual(self.deps.calls, [])

    def test_rejects_a_non_root_owned_runtime_before_execution(self):
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing runtime")

        with (
            patch.object(zeek, "KEY_SHA256", hashlib.sha256(TEST_KEY).hexdigest()),
            self.root_owned_runtime(overrides={binary: {"st_uid": 1000}}),
            self.assertRaisesRegex(ValueError, "not root-owned"),
        ):
            zeek.ensure_zeek(self.deps)

        self.assertEqual(self.deps.calls, [])

    def test_rejects_a_group_writable_runtime_directory_before_execution(self):
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing runtime")
        binary_directory = binary.parent

        with (
            patch.object(zeek, "KEY_SHA256", hashlib.sha256(TEST_KEY).hexdigest()),
            self.root_owned_runtime(
                overrides={
                    binary_directory: {
                        "st_mode": binary_directory.stat().st_mode | stat.S_IWGRP
                    }
                }
            ),
            self.assertRaisesRegex(ValueError, "group/world-writable"),
        ):
            zeek.ensure_zeek(self.deps)

        self.assertEqual(self.deps.calls, [])

    def test_rejects_a_symlink_in_the_existing_runtime_path(self):
        target = self.deps.path("/opt/zeek/bin/zeek-real")
        target.parent.mkdir(parents=True)
        target.write_bytes(b"existing runtime")
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.symlink_to(target.name)

        with self.assertRaisesRegex(ValueError, "Refusing symlink"):
            self.ensure_with_test_key()

        self.assertEqual(self.deps.calls, [])

    def test_rejects_a_prerelease_zeek_version(self):
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing runtime")
        self.deps.versions[str(binary)] = "zeek version 9.3.0-rc1\n"

        with self.assertRaisesRegex(ValueError, "stable Zeek 9 or newer"):
            self.ensure_with_test_key()

        self.assertEqual(self.deps.install_calls, [])

    def test_refuses_an_unowned_zeek_directory_before_apt(self):
        self.deps.path("/opt/zeek").mkdir(parents=True)

        with self.assertRaisesRegex(ValueError, "unowned /opt/zeek"):
            zeek.ensure_zeek(self.deps)

        self.assertEqual(self.deps.install_calls, [])
        self.assertEqual(self.deps.calls, [])

    def test_installs_the_runtime_for_supported_releases_and_architectures(self):
        for distribution, architecture in (
            ("12", "amd64"),
            ("12", "arm64"),
            ("13", "amd64"),
            ("13", "arm64"),
        ):
            with self.subTest(distribution=distribution, architecture=architecture):
                deps = FakeDependencies(distribution, architecture)
                self.addCleanup(deps.close)

                version = self.ensure_with_test_key(deps)

                self.assertEqual(version, "9.0.0")
                self.assertEqual(
                    deps.install_calls,
                    [["ca-certificates", "curl"], ["zeek-9.0-core"]],
                )
                updates = [
                    call
                    for call in deps.calls
                    if call[0] == "run" and call[1] == ("apt-get", "update")
                ]
                self.assertEqual(len(updates), 2)
                self.assertIn("/opt/zeek", deps.data["directories"])
                source = deps.path(zeek.SOURCE_PATH).read_text()
                self.assertIn(f"arch={architecture}", source)
                self.assertIn(
                    f"https://download.opensuse.org/repositories/security:/zeek/Debian_{distribution}/ /",
                    source,
                )
                self.assertIn(f"signed-by={zeek.KEY_PATH}", source)
                self.assertEqual(deps.path(zeek.KEY_PATH).read_bytes(), TEST_KEY)

    def test_retries_an_owned_zeek_directory_without_binary(self):
        self.deps.path("/opt/zeek").mkdir(parents=True)
        self.deps.data["directories"].append("/opt/zeek")

        version = self.ensure_with_test_key()

        self.assertEqual(version, "9.0.0")
        self.assertIn(["zeek-9.0-core"], self.deps.install_calls)

    def test_finishes_a_recorded_partial_package_before_reusing_runtime(self):
        binary = self.deps.path("/opt/zeek/bin/zeek")
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing runtime")
        self.deps.versions[str(binary)] = "zeek version 9.2.1\n"
        self.deps.data["packages"] = ["zeek-9.0-core:amd64"]
        self.deps.present_packages.add("zeek-9.0-core:amd64")

        version = self.ensure_with_test_key()

        self.assertEqual(version, "9.0.0")
        self.assertEqual(self.deps.install_calls, [["zeek-9.0-core"]])
        self.assertIn("zeek-9.0-core:amd64", self.deps.installed_packages)
        self.assertEqual(self.deps.data["accounts"]["zeek_group"], 988)

    def test_rejects_a_rotated_key_before_writing_apt_sources(self):
        with self.assertRaisesRegex(ValueError, "key checksum changed"):
            zeek.ensure_zeek(self.deps)

        self.assertEqual(self.deps.install_calls, [["ca-certificates", "curl"]])
        self.assertEqual(self.deps.files, {})
        self.assertFalse(self.deps.path(zeek.SOURCE_PATH).exists())

    def test_refuses_an_unowned_keyring_file(self):
        key = self.deps.path(zeek.KEY_PATH)
        key.parent.mkdir(parents=True)
        key.write_bytes(b"existing key")

        with (
            patch.object(zeek, "KEY_SHA256", hashlib.sha256(TEST_KEY).hexdigest()),
            self.assertRaisesRegex(ValueError, "unowned file"),
        ):
            zeek.ensure_zeek(self.deps)

        self.assertEqual(self.deps.install_calls, [["ca-certificates", "curl"]])
        self.assertFalse(self.deps.path(zeek.SOURCE_PATH).exists())

    def test_records_only_a_zeek_group_created_by_package_install(self):
        self.ensure_with_test_key()

        self.assertEqual(self.deps.data["accounts"]["zeek_group"], 988)

    def test_preserves_a_preexisting_zeek_group(self):
        deps = FakeDependencies(group_gid=1042)
        self.addCleanup(deps.close)

        self.ensure_with_test_key(deps)

        self.assertNotIn("zeek_group", deps.data["accounts"])
        self.assertEqual(deps.group_gid, 1042)

    def test_rejects_unsupported_distribution_or_architecture(self):
        for distribution, architecture in (("11", "amd64"), ("13", "i386")):
            with self.subTest(distribution=distribution, architecture=architecture):
                deps = FakeDependencies(distribution, architecture)
                self.addCleanup(deps.close)
                with self.assertRaises(ValueError):
                    self.ensure_with_test_key(deps)
                self.assertEqual(deps.install_calls, [])


if __name__ == "__main__":
    unittest.main()

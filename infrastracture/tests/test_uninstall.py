import json
import os
import shutil
import stat
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
from helpers import files, small_config, source_bundle

import render


class UninstallTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.work = Path(self.scratch.name)
        config = small_config(render.load_config(ROOT / "settings.example.toml"))
        self.bundle = self.work / "bundle"
        render.write_bundle(self.bundle, render.render(config))
        self.root = self.work / "host"
        self.root.mkdir()

    def make(self, role="router"):
        return source_bundle(role, self.bundle / role, self.root)

    def path(self, absolute):
        return self.root / absolute.lstrip("/")

    def write(self, absolute, data, mode=0o640):
        target = self.path(absolute)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data if isinstance(data, bytes) else data.encode())
        target.chmod(mode)
        return target

    def install_router(self):
        installer = self.make()
        installer.plan()
        installer.install()
        return installer

    def tree_state(self):
        state = {}
        for path in sorted(self.root.rglob("*")):
            relative = str(path.relative_to(self.root))
            if path.is_symlink():
                state[relative] = ("symlink", str(path.readlink()))
            elif path.is_dir():
                state[relative] = (
                    "directory",
                    stat.S_IMODE(path.stat().st_mode),
                    path.stat().st_uid,
                    path.stat().st_gid,
                )
            elif path.is_file():
                state[relative] = (
                    "file",
                    path.read_bytes(),
                    stat.S_IMODE(path.stat().st_mode),
                    path.stat().st_uid,
                    path.stat().st_gid,
                )
        return state

    def installed_state(self):
        return {
            path: state
            for path, state in self.tree_state().items()
            if not path.startswith("var/backups/")
        }

    def transaction_backup(self):
        manifest = json.loads(self.make().manifest.read_text())
        backup = Path(manifest["rollback"]["backup"])
        if backup.is_absolute():
            backup = self.path(str(backup))
        return backup

    def test_reinstall_keeps_first_install_baseline(self):
        old = self.write(
            "/etc/network/interfaces",
            b"auto lo\niface lo inet loopback\n",
            0o640,
        )
        self.install_router()

        source = self.bundle / "router/interfaces"
        source.write_bytes(source.read_bytes() + b"# updated bundle\n")
        repeated = self.make()
        repeated.plan()
        repeated.install()

        repeated.uninstall()
        self.assertEqual(old.read_bytes(), b"auto lo\niface lo inet loopback\n")
        self.assertEqual(stat.S_IMODE(old.stat().st_mode), 0o640)
        self.assertFalse(self.path("/etc/sysctl.d/90-forcad-router.conf").exists())
        self.assertFalse(self.make().manifest.exists())

        repeated.uninstall()
        self.assertEqual(old.read_bytes(), b"auto lo\niface lo inet loopback\n")

    def test_dry_run_leaves_files_and_manifest_unchanged(self):
        self.write("/etc/network/interfaces", "auto lo\niface lo inet loopback\n")
        self.install_router()
        before = self.tree_state()

        self.make().uninstall(dry_run=True)

        self.assertEqual(self.tree_state(), before)

    def test_uninstall_removes_created_directories_and_history(self):
        self.write("/etc/network/interfaces", "auto lo\niface lo inet loopback\n")
        self.install_router()
        self.make().uninstall()
        for directory in (
            "/etc/forcad",
            "/etc/sysctl.d",
            "/etc/systemd",
            "/var/backups/forcad",
        ):
            self.assertFalse(self.path(directory).exists(), directory)

    def test_history_cleanup_preserves_unowned_content(self):
        self.install_router()
        note = self.write("/var/backups/forcad/router/operator.txt", "keep")
        self.make().uninstall()
        self.assertEqual(note.read_text(), "keep")
        self.assertEqual(list(note.parent.iterdir()), [note])

    def test_legacy_backup_layout_is_cleaned_after_successful_restore(self):
        self.install_router()
        self.path("/var/backups/forcad/router/directories-owned.json").unlink()
        self.make().uninstall()
        self.assertFalse(self.path("/var/backups/forcad").exists())

    def test_preexisting_backup_directory_is_preserved(self):
        directory = self.path("/var/backups/forcad/router")
        directory.mkdir(parents=True)
        self.install_router()
        self.make().uninstall()
        self.assertTrue(directory.is_dir())
        self.assertEqual(list(directory.iterdir()), [])

    def test_preflight_returns_a_compact_plan_without_changes(self):
        self.write("/etc/network/interfaces", "auto lo\niface lo inet loopback\n")
        self.install_router()
        before = self.tree_state()

        plan = self.make().preflight_uninstall()

        self.assertEqual(self.tree_state(), before)
        self.assertTrue(plan["installed"])
        self.assertFalse(plan["conflicts"])
        self.assertTrue(plan["actions"])
        self.assertTrue(all("data" not in action for action in plan["actions"]))

    def test_drift_aborts_without_force_and_force_preserves_changed_file(self):
        original = self.write(
            "/etc/network/interfaces",
            b"auto lo\niface lo inet loopback\n",
            0o640,
        )
        self.install_router()
        changed = self.path("/etc/network/interfaces")
        changed_data = b"operator change\n"
        changed.write_bytes(changed_data)
        changed.chmod(0o600)
        before = self.tree_state()

        with self.assertRaises(ValueError):
            self.make().uninstall()
        self.assertEqual(self.tree_state(), before)

        self.make().uninstall(force=True)
        self.assertEqual(original.read_bytes(), b"auto lo\niface lo inet loopback\n")
        self.assertEqual(stat.S_IMODE(original.stat().st_mode), 0o640)
        self.assertNotEqual(changed.read_bytes(), changed_data)
        preserved = [
            path
            for path in self.path("/var/backups/forcad/router").rglob("*")
            if path.is_file() and path.read_bytes() == changed_data
        ]
        self.assertTrue(preserved, "force must back up drift before removing it")
        self.assertTrue(
            all(stat.S_IMODE(path.stat().st_mode) == 0o600 for path in preserved)
        )

    def test_owned_modified_config_is_removed_without_force(self):
        self.install_router()
        self.path("/etc/sysctl.d/90-forcad-router.conf").write_text("# changed\n")
        self.make().uninstall()
        self.assertFalse(self.path("/etc/sysctl.d/90-forcad-router.conf").exists())
        self.assertFalse(self.path("/var/backups/forcad/router").exists())

    def test_uninstall_keeps_unowned_files_and_nonempty_directories(self):
        self.install_router()
        note = self.write("/etc/forcad/router/local.conf", b"keep\n", 0o640)
        custom = self.write("/etc/systemd/system/custom.service", b"keep\n", 0o644)

        self.make().uninstall()

        self.assertEqual(note.read_bytes(), b"keep\n")
        self.assertEqual(custom.read_bytes(), b"keep\n")
        self.assertTrue(note.parent.is_dir())
        self.assertTrue(custom.parent.is_dir())

    def test_failed_restore_rolls_back_to_installed_state(self):
        self.write("/etc/network/interfaces", b"original interfaces\n", 0o640)
        self.write("/etc/sysctl.d/90-forcad-router.conf", b"original sysctl\n", 0o600)
        self.install_router()
        before = self.installed_state()
        backup_root = self.path("/var/backups/forcad/router")
        backup_count = sum(path.is_dir() for path in backup_root.iterdir())
        original_write = files.atomic_write
        calls = 0

        def fail_once(*args):
            nonlocal calls
            calls += 1
            if calls == 2:
                raise OSError("fixture restore failure")
            return original_write(*args)

        with (
            patch.object(files, "atomic_write", side_effect=fail_once),
            self.assertRaisesRegex(OSError, "fixture restore failure"),
        ):
            self.make().uninstall()

        self.assertEqual(self.installed_state(), before)
        self.assertGreater(
            sum(path.is_dir() for path in backup_root.iterdir()), backup_count
        )

    def test_persistent_restore_failure_rolls_back_earlier_file(self):
        interface = self.write(
            "/etc/network/interfaces", b"original interfaces\n", 0o640
        )
        blocked = self.write(
            "/etc/sysctl.d/90-forcad-router.conf", b"original sysctl\n", 0o600
        )
        self.install_router()
        before = self.installed_state()
        original_write = files.atomic_write
        calls = []

        def fail_persistently(path, *args):
            path = Path(path)
            calls.append(path)
            if path == blocked:
                raise OSError("persistent fixture restore failure")
            return original_write(path, *args)

        with (
            patch.object(files, "atomic_write", side_effect=fail_persistently),
            self.assertRaisesRegex(OSError, "persistent fixture restore failure"),
        ):
            self.make().uninstall()

        self.assertLess(calls.index(interface), calls.index(blocked))
        self.assertEqual(self.installed_state(), before)

    def test_missing_backup_manifest_entry_refuses_before_file_changes(self):
        self.write("/etc/network/interfaces", b"original interfaces\n", 0o640)
        self.install_router()
        files_manifest = self.transaction_backup() / "files.json"
        records = json.loads(files_manifest.read_text())
        self.assertGreater(len(records), 1)
        installed = json.loads(self.make().manifest.read_text())
        self.assertIn("files_sha256", installed["rollback"])
        self.assertIn("directories_sha256", installed["rollback"])
        records.pop(0)
        files_manifest.write_text(json.dumps(records, indent=2) + "\n")
        before = self.installed_state()

        with self.assertRaises(ValueError):
            self.make().uninstall()

        self.assertEqual(self.installed_state(), before)

    def test_missing_backup_refuses_before_changing_installed_files(self):
        self.write("/etc/network/interfaces", b"original interfaces\n", 0o640)
        self.install_router()
        before = self.tree_state()
        missing = self.transaction_backup() / "etc/network/interfaces"
        missing.unlink()

        with self.assertRaises((OSError, ValueError)):
            self.make().uninstall()

        before_without_backup = {
            key: value
            for key, value in before.items()
            if key != str(missing.relative_to(self.root))
        }
        self.assertEqual(self.tree_state(), before_without_backup)

    def test_corrupt_backup_refuses_before_changing_installed_files(self):
        self.write("/etc/network/interfaces", b"original interfaces\n", 0o640)
        self.install_router()
        backup = self.transaction_backup() / "etc/network/interfaces"
        backup.write_bytes(b"corrupt backup\n")
        before = self.tree_state()

        with self.assertRaises(ValueError):
            self.make().uninstall()

        self.assertEqual(self.tree_state(), before)

    def test_legacy_manifest_with_competing_backups_is_not_guessed(self):
        self.write("/etc/network/interfaces", b"original interfaces\n", 0o640)
        self.install_router()
        manifest_path = self.make().manifest
        manifest = json.loads(manifest_path.read_text())
        legacy = {key: manifest[key] for key in ("config", "options", "files")}
        manifest_path.write_text(json.dumps(legacy, indent=2) + "\n")

        backup_root = self.path("/var/backups/forcad/router")
        candidates = [path for path in backup_root.iterdir() if path.is_dir()]
        self.assertTrue(candidates)
        shutil.copytree(candidates[0], backup_root / "other")
        before = self.tree_state()

        with self.assertRaises(ValueError):
            self.make().uninstall()

        self.assertEqual(self.tree_state(), before)

    def test_missing_manifest_is_a_noop(self):
        note = self.write("/etc/forcad/router/local.conf", b"keep\n")
        before = self.tree_state()

        self.make().uninstall()

        self.assertEqual(self.tree_state(), before)
        self.assertEqual(note.read_bytes(), b"keep\n")

    def test_new_directories_use_recorded_group(self):
        groups = [group for group in os.getgroups() if group != os.getegid()]
        if not groups and os.geteuid() != 0:
            self.skipTest("Requires an additional group or container root")
        group = groups[0] if groups else 12345
        parent = self.path("/etc/forcad")
        parent.mkdir(parents=True)
        os.chown(parent, -1, group)
        installer = self.make()
        installer.plan(game_metrics=True)
        installer.install()
        self.assertEqual(self.path("/etc/forcad/monitoring").stat().st_gid, group)
        installer.uninstall(dry_run=True)
        installer.uninstall()
        self.assertEqual(parent.stat().st_gid, group)

    def test_uninstall_restores_preexisting_directory_mode(self):
        directory = self.path("/etc/forcad")
        directory.mkdir(parents=True)
        directory.chmod(0o750)
        self.install_router()
        self.assertEqual(stat.S_IMODE(directory.stat().st_mode), 0o755)

        self.make().uninstall()

        self.assertTrue(directory.is_dir())
        self.assertEqual(stat.S_IMODE(directory.stat().st_mode), 0o750)

    def test_directory_owner_drift_refuses_before_file_changes(self):
        directory = self.path("/etc/forcad")
        directory.mkdir(parents=True)
        directory.chmod(0o750)
        self.install_router()
        before = self.installed_state()
        original_stat = Path.stat

        def different_owner(path, *args, **kwargs):
            info = original_stat(path, *args, **kwargs)
            if path != directory:
                return info
            fields = list(info)
            fields[4] = info.st_uid + 1
            return os.stat_result(fields)

        with (
            patch.object(Path, "stat", autospec=True, side_effect=different_owner),
            self.assertRaises(ValueError),
        ):
            self.make().uninstall()

        self.assertEqual(self.installed_state(), before)

    def test_other_role_plan_refuses_after_router_install(self):
        self.install_router()
        before = self.tree_state()

        with self.assertRaisesRegex(ValueError, "router.*already owns"):
            self.make("vpn").plan()

        self.assertEqual(self.tree_state(), before)


if __name__ == "__main__":
    unittest.main()

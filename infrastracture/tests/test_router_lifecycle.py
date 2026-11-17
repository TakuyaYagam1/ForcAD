"""File-backed lifecycle tests with isolated runtime adapters."""

import copy
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from helpers import small_config, source_bundle

import render


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        work = Path(self.scratch.name)
        self.root = work / "host"
        self.root.mkdir()
        self.directory = work / "bundle"
        config = render.load_config(
            Path(render.__file__).parent / "settings.example.toml"
        )
        small_config(config)
        render.write_bundle(self.directory, render.render(config))
        self.original = self.root / "etc/network/interfaces"
        self.original.parent.mkdir(parents=True)
        self.original.write_text("auto lo\niface lo inet loopback\n")
        self.old_interfaces = self.original.read_bytes()
        self.make()
        from routerlib.lifecycle import Lifecycle

        self.Lifecycle = Lifecycle
        self.runtime = {
            "network": {"addresses": ["old"]},
            "system": {
                "units": {"firewall": "inactive"},
                "tables": {"router": None},
                "options": {},
            },
        }
        self.network = Mock()
        self.system = Mock()
        self.network.snapshot.side_effect = lambda **kwargs: copy.deepcopy(
            self.runtime["network"]
        )
        self.system.snapshot.side_effect = lambda options: copy.deepcopy(
            self.runtime["system"]
        )
        for name, adapter in (("Network", self.network), ("System", self.system)):
            patcher = patch(f"routerlib.lifecycle.{name}", return_value=adapter)
            patcher.start()
            self.addCleanup(patcher.stop)

    def make(self):
        bundle = source_bundle("router", self.directory / "router", self.root)
        bundle.files_only = False
        bundle.plan()
        return bundle

    def test_install_starts_services_and_uninstall_restores_first_baseline(self):
        first = self.make()
        first.install()
        self.system.prepare.assert_called_once()
        self.network.apply.assert_called_once()
        self.system.apply.assert_called_once()
        self.network.verify.assert_called_once()
        self.runtime["network"] = {"addresses": ["installed"]}
        self.runtime["system"]["tables"]["router"] = "installed rules"
        second = self.make()
        second.install()
        journal = self.Lifecycle(second).load()
        self.assertEqual(journal["baseline"]["network"], {"addresses": ["old"]})
        self.assertIsNone(journal["baseline"]["system"]["tables"]["router"])
        second.uninstall()
        self.network.restore.assert_called_once_with(
            {"addresses": ["old"]}, applied={"addresses": ["installed"]}, force=False
        )
        self.assertEqual(self.original.read_bytes(), self.old_interfaces)
        self.assertFalse(second.manifest.exists())
        self.assertFalse(self.Lifecycle(second).journal.exists())

    def test_apply_failure_restores_files_and_before_live_state(self):
        bundle = self.make()
        self.system.apply.side_effect = ValueError("service startup failed")
        with self.assertRaisesRegex(
            ValueError, "previous files and live state restored"
        ):
            bundle.install()
        self.assertEqual(self.original.read_bytes(), self.old_interfaces)
        self.assertFalse(bundle.manifest.exists())
        self.network.restore.assert_called_once_with(self.runtime["network"])
        self.system.restore.assert_called_once_with(self.runtime["system"])
        self.assertFalse(self.Lifecycle(bundle).journal.exists())

    def test_uninstall_does_not_require_install_prerequisites(self):
        bundle = self.make()
        bundle.install()
        self.system.preflight.side_effect = ValueError("metrics dependency removed")
        self.system.preflight.reset_mock()
        bundle.uninstall()
        self.system.preflight.assert_not_called()
        self.system.stop.assert_called()
        self.system.restore.assert_called_once()
        self.assertFalse(bundle.path("/var/backups/forcad/router").exists())

    def test_failed_reinstall_keeps_previous_installation(self):
        self.make().install()
        before = self.original.read_bytes()
        source = self.directory / "router/interfaces"
        source.write_bytes(source.read_bytes() + b"# updated\n")
        self.system.apply.side_effect = ValueError("failure")
        bundle = self.make()
        with self.assertRaisesRegex(
            ValueError, "previous files and live state restored"
        ):
            bundle.install()
        self.assertEqual(self.original.read_bytes(), before)
        self.assertEqual(self.Lifecycle(bundle).load()["phase"], "active")
        self.assertTrue(bundle.manifest.exists())

    def test_uninstall_drift_and_dry_run_do_not_stop_services(self):
        bundle = self.make()
        bundle.install()
        self.system.reset_mock()
        bundle.uninstall(dry_run=True)
        self.system.stop.assert_not_called()
        self.assertTrue(bundle.manifest.exists())
        self.original.write_text("# manual change\n")
        with self.assertRaisesRegex(ValueError, "Files changed"):
            bundle.uninstall()
        self.system.stop.assert_not_called()
        self.assertTrue(self.Lifecycle(bundle).journal.exists())

    def test_live_installation_cannot_be_removed_with_files_only(self):
        bundle = self.make()
        bundle.install()
        bundle.files_only = True
        with self.assertRaisesRegex(ValueError, "live installation"):
            bundle.uninstall()
        self.system.stop.assert_not_called()

    def test_next_install_after_uninstall_captures_new_baseline(self):
        bundle = self.make()
        bundle.install()
        bundle.uninstall()
        self.runtime["network"] = {"addresses": ["new manual state"]}
        bundle = self.make()
        bundle.install()
        self.assertEqual(
            self.Lifecycle(bundle).load()["baseline"]["network"],
            {"addresses": ["new manual state"]},
        )

    def test_preflight_failure_changes_no_installed_files(self):
        bundle = self.make()
        self.system.preflight.side_effect = ValueError("foreign firewall")
        with self.assertRaisesRegex(ValueError, "foreign firewall"):
            bundle.install()
        self.assertEqual(self.original.read_bytes(), self.old_interfaces)
        self.assertFalse(bundle.manifest.exists())
        self.system.prepare.assert_not_called()

    def test_incomplete_recovery_keeps_journal_and_backup(self):
        bundle = self.make()
        self.system.apply.side_effect = ValueError("start failed")
        self.system.restore.side_effect = ValueError("restore failed")
        with self.assertRaisesRegex(ValueError, "Recovery incomplete"):
            bundle.install()
        self.assertTrue(self.Lifecycle(bundle).journal.exists())
        with self.assertRaisesRegex(ValueError, "Incomplete or corrupt"):
            self.Lifecycle(bundle).load()

    def interrupted_uninstall(self):
        bundle = self.make()
        bundle.install()
        lifecycle = self.Lifecycle(bundle)
        record = lifecycle.load()
        record["phase"] = "uninstalling"
        record["before"] = copy.deepcopy(self.runtime)
        lifecycle.save(record)
        self.system.reset_mock()
        return bundle, lifecycle, record

    def test_resume_uninstall_requires_explicit_option(self):
        bundle, lifecycle, _ = self.interrupted_uninstall()
        with self.assertRaisesRegex(ValueError, "uninstalling"):
            bundle.uninstall()
        self.system.stop.assert_not_called()
        original_journal = lifecycle.journal.read_bytes()
        bundle.uninstall(resume=True, dry_run=True)
        self.system.stop.assert_not_called()
        self.assertEqual(lifecycle.journal.read_bytes(), original_journal)
        bundle.uninstall(resume=True)
        self.system.stop.assert_called()
        self.assertFalse(bundle.manifest.exists())
        self.assertFalse(lifecycle.journal.exists())
        self.assertEqual(self.original.read_bytes(), self.old_interfaces)

    def test_resume_does_not_accept_corrupt_journal(self):
        bundle, lifecycle, _ = self.interrupted_uninstall()
        lifecycle.journal.write_text(
            lifecycle.journal.read_text().replace('"uninstalling"', '"active"')
        )
        with self.assertRaisesRegex(ValueError, "corrupt"):
            bundle.uninstall(resume=True)
        self.system.stop.assert_not_called()

    def test_resume_does_not_accept_interrupted_install(self):
        bundle, lifecycle, record = self.interrupted_uninstall()
        record["phase"] = "applying"
        lifecycle.save(record)
        with self.assertRaisesRegex(ValueError, "applying"):
            bundle.uninstall(resume=True)
        self.system.stop.assert_not_called()

    def test_failed_resume_keeps_unfinished_journal(self):
        bundle, lifecycle, record = self.interrupted_uninstall()
        self.network.restore.side_effect = [ValueError("restore failed"), None]
        with self.assertRaisesRegex(
            ValueError, "previous files and live state restored"
        ):
            bundle.uninstall(resume=True)
        self.assertTrue(bundle.manifest.exists())
        self.assertEqual(lifecycle.load(resume=True), record)

    def test_resume_requires_original_file_manifest(self):
        bundle, _, _ = self.interrupted_uninstall()
        bundle.manifest.unlink()
        with self.assertRaisesRegex(ValueError, "no file manifest"):
            bundle.uninstall(resume=True)
        self.system.stop.assert_not_called()

    def test_missing_journal_cannot_downgrade_live_uninstall_to_files_only(self):
        bundle = self.make()
        bundle.install()
        self.Lifecycle(bundle).journal.unlink()
        with self.assertRaisesRegex(ValueError, "journal is missing"):
            bundle.uninstall()
        self.assertTrue(bundle.manifest.exists())
        self.system.stop.assert_not_called()
        bundle.files_only = True
        with self.assertRaisesRegex(ValueError, "live installation"):
            bundle.uninstall()

    def test_runtime_before_force_uninstall_is_kept_in_backup(self):
        bundle = self.make()
        bundle.install()
        self.runtime["network"] = {"manual": "settings"}
        bundle.uninstall(force=True)
        records = list(
            (self.root / "var/backups/forcad/router").glob("apply-*/live-before.json")
        )
        self.assertTrue(
            any('"manual": "settings"' in path.read_text() for path in records)
        )

    def test_failed_uninstall_restores_installed_files_and_live_state(self):
        bundle = self.make()
        bundle.install()
        installed = self.original.read_bytes()
        self.runtime["network"] = {"addresses": ["installed"]}
        self.system.restore.side_effect = [ValueError("restore service failed"), None]
        with self.assertRaisesRegex(
            ValueError, "previous files and live state restored"
        ):
            bundle.uninstall()
        self.assertEqual(self.original.read_bytes(), installed)
        self.assertTrue(bundle.manifest.exists())
        self.network.restore.assert_called_with(
            {"addresses": ["installed"]}, allowed_current={"addresses": ["old"]}
        )
        self.assertEqual(self.Lifecycle(bundle).load()["phase"], "active")

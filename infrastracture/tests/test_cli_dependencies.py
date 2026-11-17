import contextlib
import io
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import helpers  # noqa: F401

from hostlib import cli


class CLIDependencyTests(unittest.TestCase):
    def invoke(self, role, arguments, failure=None):
        with tempfile.TemporaryDirectory() as directory:
            events = []
            bundle = Mock(role=role, manifest=Path(directory) / "absent")
            bundle.plan.side_effect = lambda *args: events.append("plan")
            bundle.validate.side_effect = lambda: events.append("validate")
            bundle.install.side_effect = lambda: events.append("install")

            def uninstall(**kwargs):
                events.append("uninstall")
                if failure:
                    raise ValueError("restore failed")

            bundle.uninstall.side_effect = uninstall
            dependencies = Mock()
            dependencies.install.side_effect = lambda **kwargs: events.append(
                "dependencies"
            )
            dependencies.uninstall.side_effect = lambda **kwargs: events.append(
                "cleanup"
            )
            with (
                patch.object(cli.os, "geteuid", return_value=0),
                patch.object(cli.os, "umask"),
                patch.object(cli, "Dependencies", return_value=dependencies) as factory,
                patch("sys.argv", ["host.py", *arguments]),
                contextlib.redirect_stderr(io.StringIO()),
            ):
                if failure:
                    with self.assertRaises(SystemExit):
                        cli.main(lambda _: bundle, directory)
                else:
                    cli.main(lambda _: bundle, directory)
            return events, dependencies, factory

    def test_dependencies_before_validation_for_router_and_vpn(self):
        for role, arguments in (
            ("router", ["install", "--game-metrics", "--network-metrics"]),
            ("vpn", ["install", "--wireguard-dir", "/path/to/server"]),
        ):
            with self.subTest(role=role):
                events, deps, _ = self.invoke(role, arguments)
                self.assertEqual(
                    events, ["plan", "dependencies", "validate", "install"]
                )
                deps.install.assert_called_once_with(game_metrics=role == "router")

    def test_purge_only_after_successful_host_restore(self):
        events, deps, _ = self.invoke("router", ["uninstall", "--purge-packages"])
        self.assertEqual(events, ["uninstall", "cleanup"])
        deps.uninstall.assert_called_once_with(choice="purge", dry_run=False)
        events, deps, factory = self.invoke(
            "router", ["uninstall", "--purge-packages"], failure=True
        )
        self.assertEqual(events, ["uninstall"])
        factory.assert_not_called()
        deps.uninstall.assert_not_called()

    def test_dry_run_does_not_purge(self):
        _, deps, _ = self.invoke("vpn", ["uninstall", "--dry-run", "--purge-packages"])
        deps.uninstall.assert_called_once_with(choice="purge", dry_run=True)

    def test_forcad_uses_its_existing_bootstrap(self):
        events, _, factory = self.invoke("forcad", ["install"])
        self.assertEqual(events, ["plan", "validate", "install"])
        factory.assert_not_called()

    def test_test_does_not_install_dependencies(self):
        _, _, factory = self.invoke("router", ["test"])
        factory.assert_not_called()

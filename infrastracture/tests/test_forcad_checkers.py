import stat
import tempfile
import unittest
from pathlib import Path

from helpers import ROOT

from forcad.install_checkers import install_checkers


class CheckerPermissionsTests(unittest.TestCase):
    def test_only_configured_entrypoints_get_execute_permission(self):
        with tempfile.TemporaryDirectory() as directory:
            bundle = Path(directory) / "bundle"
            target = Path(directory) / "checkout"
            source = bundle / "checkers/service one"
            source.mkdir(parents=True, mode=0o700)
            (bundle / "config.yml").write_text(
                'tasks:\n  - checker: "service one/run"\n'
            )
            entry = source / "run"
            entry.write_bytes(
                (ROOT.parent / "checkers/examples/demo/down.py").read_bytes()
            )
            entry.chmod(0o600)
            helper = source / "checker.py"
            helper.write_text("# not the entrypoint\n")
            helper.chmod(0o700)
            tree = target / "checkers"
            tree.mkdir(parents=True, mode=0o700)
            (tree / "unrelated").touch(mode=0o600)

            install_checkers(bundle, target)

            for relative, mode in (
                (".", 0o755),
                ("service one", 0o755),
                ("service one/run", 0o755),
                ("service one/checker.py", 0o644),
                ("unrelated", 0o600),
            ):
                with self.subTest(path=relative):
                    self.assertEqual(
                        stat.S_IMODE((tree / relative).stat().st_mode), mode
                    )
            self.assertEqual(stat.S_IMODE(entry.stat().st_mode), 0o600)

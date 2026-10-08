import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
from tempfile import TemporaryDirectory
from unittest import TestCase


PROJECT = Path(__file__).resolve().parents[2]
SCRIPT = PROJECT / "security" / "scan_inputs.py"
FAKE_SCANNER = '''\
import json, os, sys
from pathlib import Path

command, args = sys.argv[1], sys.argv[2:]
if command == "version":
    print("gitleaks test version")
    raise SystemExit(0)
report = next(a.split("=", 1)[1] for a in args if a.startswith("--report-path="))
mode = os.environ.get("FAKE_MODE", "ok")
event = {"command": command, "args": args}
if command == "dir":
    root = Path.cwd()
    event["cwd"] = str(root)
    event["files"] = sorted(
        str(p.relative_to(root)) for p in root.rglob("*") if p.is_file()
    )
    event["root_mode"] = root.stat().st_mode & 0o777
    event["file_modes"] = {str(p.relative_to(root)): p.stat().st_mode & 0o777
                            for p in root.rglob("*") if p.is_file()}
with open(os.environ["CALL_LOG"], "a", encoding="utf-8") as log:
    log.write(json.dumps(event) + "\\n")
if mode != "missing-history" or command != "git":
    payload = (
        [{"RuleID": "fixture-marker"}]
        if mode == "finding" and command == "dir" else []
    )
    Path(report).write_text(json.dumps(payload), encoding="utf-8")
if mode == "finding" and command == "dir":
    print("fixture-marker")
    print("fixture-marker", file=sys.stderr)
raise SystemExit(7 if mode == "fail-current" and command == "dir" else
                 1 if mode == "finding" and command == "dir" else 0)
'''


class InputScanTest(TestCase):
    def setUp(self):
        self.temp = TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / "repo"
        self.root.mkdir()
        self.reports = self.base / "reports"
        self.cache = self.base / "cache"
        self.config = self.base / "gitleaks.toml"
        self.config.write_text("[allow]\n", encoding="utf-8")
        self.calls = self.base / "calls.jsonl"
        self.scanner = self.base / "fake-gitleaks"
        self.scanner.write_text(
            f"#!{sys.executable}\n" + FAKE_SCANNER, encoding="utf-8",
        )
        self.scanner.chmod(0o700)

        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        subprocess.run([
            "git", "-C", str(self.root), "config", "user.name", "Fixture",
        ], check=True)
        subprocess.run([
            "git", "-C", str(self.root), "config", "user.email",
            "fixture@example.invalid",
        ], check=True)
        (self.root / ".gitignore").write_text(
            "ignored-untracked.cfg\ntracked-ignored.cfg\n", encoding="utf-8",
        )
        (self.root / "tracked.txt").write_text(
            "benign fixture data\n", encoding="utf-8",
        )
        (self.root / "tracked-ignored.cfg").write_text(
            "benign tracked data\n", encoding="utf-8",
        )
        (self.root / "nested").mkdir()
        (self.root / "nested" / "child.txt").write_text(
            "nested fixture\n", encoding="utf-8",
        )
        (self.root / "nested" / "hidden.txt").write_text(
            "benign symlink target\n", encoding="utf-8",
        )
        (self.root / "link-parent").mkdir()
        (self.root / "link-parent" / "hidden.txt").write_text(
            "parent fixture\n", encoding="utf-8",
        )
        subprocess.run([
            "git", "-C", str(self.root), "add", ".gitignore", "tracked.txt",
            "nested", "link-parent",
        ], check=True)
        subprocess.run([
            "git", "-C", str(self.root), "add", "-f", "tracked-ignored.cfg",
        ], check=True)
        subprocess.run([
            "git", "-C", str(self.root), "commit", "-qm", "fixture",
        ], check=True)

        shutil.rmtree(self.root / "link-parent")
        (self.root / "link-parent").symlink_to(
            self.root / "nested", target_is_directory=True,
        )
        (self.root / "untracked.txt").write_text(
            "new benign data\n", encoding="utf-8",
        )
        (self.root / "ignored-untracked.cfg").write_text(
            "excluded benign data\n", encoding="utf-8",
        )
        (self.root / "shortcut.txt").symlink_to(self.root / "tracked.txt")
        (self.root / "shortcut-dir").symlink_to(
            self.root / "nested", target_is_directory=True,
        )
        os.mkfifo(self.root / "pipe")

    def invoke(self, mode="ok"):
        env = os.environ.copy()
        env.update(
            CALL_LOG=str(self.calls), FAKE_MODE=mode,
            PYTHONDONTWRITEBYTECODE="1",
        )
        return subprocess.run([
            sys.executable, str(SCRIPT), "--root", str(self.root),
            "--scanner", str(self.scanner), "--config", str(self.config),
            "--reports", str(self.reports), "--cache", str(self.cache),
        ], env=env, capture_output=True, text=True, timeout=30, check=False)

    def events(self):
        lines = self.calls.read_text(encoding="utf-8").splitlines()
        return [json.loads(line) for line in lines]

    def test_current_inputs_keep_relative_paths_and_skip_links_and_special_files(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        current, history = self.events()
        files = set(current["files"])
        self.assertIn("tracked-ignored.cfg", files)
        self.assertIn("untracked.txt", files)
        self.assertIn("nested/child.txt", files)
        self.assertIn("nested/hidden.txt", files)
        self.assertNotIn("ignored-untracked.cfg", files)
        self.assertNotIn("shortcut.txt", files)
        self.assertNotIn("shortcut-dir/child.txt", files)
        self.assertNotIn("link-parent/hidden.txt", files)
        self.assertNotIn("pipe", files)
        self.assertEqual(Path(current["cwd"]).parent, self.cache)
        self.assertEqual(current["root_mode"], 0o700)
        self.assertTrue(all(mode == 0o600 for mode in current["file_modes"].values()))
        for event in (current, history):
            config_index = event["args"].index("--config")
            self.assertEqual(
                event["args"][config_index + 1], str(self.config.resolve()),
            )
        flags = ("--redact=100", "--ignore-gitleaks-allow", "--no-banner", "--no-color")
        for flag in flags:
            self.assertIn(flag, current["args"])
            self.assertIn(flag, history["args"])
        self.assertIn("--log-opts=--all", history["args"])
        version = (self.reports / "gitleaks-version.txt").read_text()
        self.assertEqual(version, "gitleaks test version\n")
        report = (self.reports / "gitleaks-files.json").read_text()
        self.assertEqual(json.loads(report), [])

    def test_current_finding_runs_history_without_printing_finding(self):
        result = self.invoke("finding")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual([event["command"] for event in self.events()], ["dir", "git"])
        self.assertNotIn("fixture-marker", result.stdout + result.stderr)

    def test_scanner_failure_is_nonzero_even_when_reports_are_empty(self):
        result = self.invoke("fail-current")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual([event["command"] for event in self.events()], ["dir", "git"])
        self.assertTrue((self.reports / "gitleaks-files.json").exists())
        self.assertTrue((self.reports / "gitleaks-history.json").exists())

    def test_missing_report_does_not_reuse_stale_json(self):
        self.reports.mkdir()
        for name in ("gitleaks-files.json", "gitleaks-history.json"):
            (self.reports / name).write_text("[]", encoding="utf-8")
        result = self.invoke("missing-history")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(
            "missing or invalid JSON list report: gitleaks-history.json",
            result.stderr,
        )
        self.assertFalse((self.reports / "gitleaks-history.json").exists())
        self.assertEqual([event["command"] for event in self.events()], ["dir", "git"])

    def test_shallow_checkout_fails_clearly(self):
        commit = subprocess.check_output([
            "git", "-C", str(self.root), "rev-parse", "HEAD",
        ], text=True).strip()
        (self.root / ".git" / "shallow").write_text(commit + "\n", encoding="ascii")
        result = self.invoke()
        self.assertEqual(result.returncode, 2)
        self.assertIn(
            "shallow Git checkout cannot provide complete history", result.stderr,
        )
        self.assertFalse(self.calls.exists())


if __name__ == "__main__":
    import unittest
    unittest.main()

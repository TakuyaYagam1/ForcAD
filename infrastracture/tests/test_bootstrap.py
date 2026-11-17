import os
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TEMPLATE = ROOT / "templates/host.sh.in"
BASH = shutil.which("bash")


class HostBootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.bundle = self.root / "bundle"
        self.bundle.mkdir()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.state_root = self.root / "state"
        self.lock_root = self.root / "run-lock"
        self.lock_root.mkdir()
        self.lock_file = self.lock_root / "forcad-dependencies.lock"
        self.system_var = self.root / "var"
        (self.system_var / "lib").mkdir(parents=True)
        self.os_release = self.root / "os-release"
        self.os_release.write_text('ID=debian\nVERSION_ID="12"\n')
        self.call_log = self.root / "calls.log"
        (self.bundle / "host.py").write_text("pass\n")
        self._add_tool_links()
        self._write_dpkg_query()
        self._write_apt_get()
        self._write_flock()

    def tearDown(self):
        self.temp.cleanup()

    def _add_tool_links(self):
        for name in (
            "dirname",
            "cmp",
            "awk",
            "sort",
            "mkdir",
            "chmod",
            "mktemp",
            "ln",
            "rm",
            "cat",
            "mv",
            "flock",
        ):
            source = shutil.which(name)
            if source is None:
                self.fail(f"Test host is missing {name}")
            (self.bin / name).symlink_to(source)

    def _write_dpkg_query(self):
        script = """#!/bin/bash
printf 'dpkg-query\\n' >> "$CALL_LOG"
printf '%s\\n' \\
    'installed zlib1g:amd64' \\
    'config-files obsolete-package' \\
    'not-installed absent-package' \\
    'installed bash' \\
    'installed python3-minimal:amd64'
"""
        script = script.replace("#!/bin/bash", f"#!{BASH}")
        path = self.bin / "dpkg-query"
        path.write_text(script)
        path.chmod(0o700)

    def _write_apt_get(self):
        script = """#!/bin/bash
printf 'apt-get %s\\n' "$*" >> "$CALL_LOG"
if [[ "${EXPECT_LOCK_FD:-false}" == true && ! -e /proc/$$/fd/9 ]]; then
    printf 'missing lock fd during apt-get\\n' >> "$CALL_LOG"
    exit 19
fi
if [[ "${EXPECT_LOCK_FD:-false}" == true ]] &&
    "$REAL_FLOCK" -n -c true "$LOCK_FILE" 2>/dev/null; then
    printf 'dependency lock was not held during apt-get\\n' >> "$CALL_LOG"
    exit 20
fi
if [[ "$1" == -s ]]; then
    printf 'apt-env DEBIAN_FRONTEND=%s NEEDRESTART_MODE=%s\\n' "$DEBIAN_FRONTEND" "$NEEDRESTART_MODE" >> "$CALL_LOG"
    printf '%s\\n' \\
        'Inst python3 [3.11.2-1] (3.11.2-1 Debian:12/stable [amd64])' \\
        'Inst python3-minimal:amd64 [3.11.2-1] (3.11.2-1 Debian:12/stable [amd64])' \\
        'Inst libpython3-stdlib:amd64 [3.11.2-1] (3.11.2-1 Debian:12/stable [amd64])' \\
        'Conf python3 (3.11.2-1 Debian:12/stable [amd64])' \\
        'Remv old-python [3.10.0-1]'
    exit 0
fi
if [[ "$1" == update && "${APT_UPDATE_FAIL:-}" == true ]]; then
    exit 17
fi
if [[ "$1" == install && "${APT_INSTALL_FAIL:-}" == true ]]; then
    exit 18
fi
if [[ "$1" == install ]]; then
    printf 'apt-env DEBIAN_FRONTEND=%s NEEDRESTART_MODE=%s\\n' "$DEBIAN_FRONTEND" "$NEEDRESTART_MODE" >> "$CALL_LOG"
    cat > "$FAKE_BIN/python3" <<'PYTHON'
#!/bin/bash
if [[ "${EXPECT_LOCK_FD:-false}" == true && ! -e /proc/$$/fd/9 ]]; then
    printf 'missing lock fd after bootstrap\\n' >> "$CALL_LOG"
    exit 19
fi
if [[ "${EXPECT_LOCK_FD:-false}" == true ]] &&
    "$REAL_FLOCK" -n -c true "$LOCK_FILE" 2>/dev/null; then
    printf 'dependency lock was not held after bootstrap\\n' >> "$CALL_LOG"
    exit 20
fi
printf 'python3' >> "$CALL_LOG"
printf ' %s' "$@" >> "$CALL_LOG"
printf '\\n' >> "$CALL_LOG"
PYTHON
    chmod 0700 "$FAKE_BIN/python3"
fi
"""
        script = script.replace("#!/bin/bash", f"#!{BASH}")
        path = self.bin / "apt-get"
        path.write_text(script)
        path.chmod(0o700)

    def _write_flock(self):
        path = self.bin / "flock"
        path.unlink()
        path.write_text(
            f"#!{BASH}\n"
            'printf \'flock %s\\n\' "$*" >> "$CALL_LOG"\n'
            'exec "$REAL_FLOCK" "$@"\n'
        )
        path.chmod(0o700)

    def _write_python(self):
        path = self.bin / "python3"
        path.write_text(
            f"#!{BASH}\n"
            'if [[ "${EXPECT_LOCK_FD:-false}" == true && ! -e /proc/$$/fd/9 ]]; then\n'
            "    printf 'missing lock fd during python\\n' >> \"$CALL_LOG\"\n"
            "    exit 19\n"
            "fi\n"
            'if [[ "${EXPECT_LOCK_FD:-false}" == true ]] &&\n'
            '    "$REAL_FLOCK" -n -c true "$LOCK_FILE" 2>/dev/null; then\n'
            '    printf "dependency lock was not held during python\\n" >> "$CALL_LOG"\n'
            "    exit 20\n"
            "fi\n"
            "printf 'python3' >> \"$CALL_LOG\"\n"
            'printf \' %s\' "$@" >> "$CALL_LOG"\n'
            "printf '\\n' >> \"$CALL_LOG\"\n"
        )
        path.chmod(0o700)

    def _write_dependency_receipt(self, role="router", owner=None):
        state = self.state_root / role
        state.mkdir(parents=True, mode=0o700)
        owner_file = state / "owner"
        owner_file.write_text(
            owner if owner is not None else f"forcad-dependencies-v1:{role}\n"
        )
        owner_file.chmod(0o600)
        state_file = state / "state.json"
        state_file.write_text("{}\n")
        state_file.chmod(0o600)
        return state

    def _script(self, role="router", action="install"):
        source = TEMPLATE.read_text()
        source = source.replace("@@ROLE@@", role).replace("@@ACTION@@", action)
        source = source.replace("/run/lock", str(self.lock_root))
        source = source.replace(
            'if [[ "$EUID" -ne 0 && "$help_requested" != true ]]; then',
            'if [[ 0 -ne 0 && "$help_requested" != true ]]; then',
        )
        source = source.replace("/var/lib/forcad-dependencies", str(self.state_root))
        source = source.replace(
            "for directory in /var /var/lib; do",
            f"for directory in '{self.system_var}' '{self.system_var / 'lib'}'; do",
        )
        source = source.replace("/etc/os-release", str(self.os_release))
        path = self.bundle / f"{action}.sh"
        path.write_text(source)
        path.chmod(0o700)
        return path

    def _run(self, script, *arguments, **extra_env):
        environment = {
            "PATH": str(self.bin),
            "CALL_LOG": str(self.call_log),
            "FAKE_BIN": str(self.bin),
            "REAL_FLOCK": shutil.which("flock"),
            "LOCK_FILE": str(self.lock_file),
            **os.environ,
            **extra_env,
        }
        environment["PATH"] = str(self.bin)
        return subprocess.run(
            [shutil.which("bash"), str(script), *arguments],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

    def _calls(self):
        if not self.call_log.exists():
            return []
        return self.call_log.read_text().splitlines()

    def test_install_bootstraps_python_and_records_installed_packages(self):
        script = self._script()

        result = self._run(
            script,
            "--files-only",
            "--network-metrics",
            EXPECT_LOCK_FD="true",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self._calls()
        self.assertEqual(
            [call for call in calls if call.startswith("apt-get ")],
            [
                "apt-get update",
                "apt-get -s install -y --no-install-recommends --no-upgrade --no-remove python3",
                "apt-get install -y --no-install-recommends --no-upgrade --no-remove python3",
            ],
        )
        self.assertEqual(
            [call for call in calls if call.startswith("apt-env ")],
            [
                "apt-env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l",
                "apt-env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l",
            ],
        )
        self.assertIn("dpkg-query", calls)
        self.assertEqual(
            calls[-1],
            f"python3 {self.bundle / 'host.py'} install --files-only --network-metrics",
        )

        state = self.state_root / "router"
        owner = state / "owner"
        before = state / "bootstrap-before"
        candidates = state / "bootstrap-candidates"
        self.assertEqual(owner.read_text(), "forcad-dependencies-v1:router\n")
        self.assertEqual(
            before.read_text(),
            "bash\nobsolete-package\npython3-minimal:amd64\nzlib1g:amd64\n",
        )
        self.assertEqual(
            candidates.read_text(),
            "libpython3-stdlib:amd64\npython3\npython3-minimal:amd64\n",
        )
        self.assertEqual(stat.S_IMODE(state.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(owner.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(before.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(candidates.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(self.lock_file.stat().st_mode), 0o600)

    def test_existing_python_skips_all_package_bootstrap_work(self):
        self._write_python()
        script = self._script()

        result = self._run(script, "--network-metrics", EXPECT_LOCK_FD="true")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self._calls()[-1:],
            [f"python3 {self.bundle / 'host.py'} install --network-metrics"],
        )
        self.assertIn("flock -x 9", self._calls())
        self.assertFalse(self.state_root.exists())
        self.assertTrue(self.lock_file.exists())

    def test_test_uninstall_and_help_never_bootstrap_python(self):
        self._write_python()
        for action, arguments in (
            ("test", ()),
            ("uninstall", ("--dry-run",)),
            ("install", ("--help",)),
        ):
            with self.subTest(action=action, arguments=arguments):
                script = self._script(action=action)
                result = self._run(script, *arguments)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertFalse(
                    any(call.startswith("apt-get ") for call in self._calls())
                )
                self.assertFalse(self.state_root.exists())
                self.assertFalse(self.lock_file.exists())
                self.assertFalse(
                    any(call.startswith("flock ") for call in self._calls())
                )
                self.assertEqual(
                    self._calls()[-1],
                    f"python3 {self.bundle / 'host.py'} {action}"
                    + (f" {' '.join(arguments)}" if arguments else ""),
                )

    def test_uninstall_without_receipt_does_not_bootstrap_python(self):
        script = self._script(action="uninstall")

        result = self._run(script)

        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(call.startswith("apt-get ") for call in self._calls()))
        self.assertFalse(self.state_root.exists())
        self.assertTrue(self.lock_file.exists())
        self.assertIn("flock -x 9", self._calls())
        self.assertFalse(any(call.startswith("python3 ") for call in self._calls()))

    def test_uninstall_receipt_recovers_python_to_finish_cleanup(self):
        state = self._write_dependency_receipt()
        script = self._script(action="uninstall")

        result = self._run(script, "--keep-packages", EXPECT_LOCK_FD="true")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            "Restoring Python to finish interrupted dependency cleanup.",
            result.stderr,
        )
        self.assertEqual(
            [call for call in self._calls() if call.startswith("apt-get ")],
            [
                "apt-get update",
                "apt-get -s install -y --no-install-recommends --no-upgrade --no-remove python3",
                "apt-get install -y --no-install-recommends --no-upgrade --no-remove python3",
            ],
        )
        self.assertEqual(
            self._calls()[-1],
            f"python3 {self.bundle / 'host.py'} uninstall --keep-packages",
        )
        self.assertEqual(
            (state / "owner").read_text(), "forcad-dependencies-v1:router\n"
        )
        self.assertTrue((state / "state.json").is_file())
        self.assertTrue((state / "bootstrap-before").is_file())
        self.assertTrue((state / "bootstrap-candidates").is_file())

    def test_uninstall_dry_run_does_not_restore_python_from_receipt(self):
        state = self._write_dependency_receipt()
        script = self._script(action="uninstall")

        result = self._run(script, "--dry-run")

        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("Restoring Python", result.stderr)
        self.assertFalse(any(call.startswith("apt-get ") for call in self._calls()))
        self.assertFalse(self.lock_file.exists())
        self.assertEqual(
            sorted(path.name for path in state.iterdir()), ["owner", "state.json"]
        )

    def test_uninstall_refuses_invalid_receipt_without_bootstrapping(self):
        state = self._write_dependency_receipt(owner="forcad-dependencies-v1:vpn\n")
        script = self._script(action="uninstall")

        result = self._run(script)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Dependency state owner does not match router", result.stderr)
        self.assertFalse(any(call.startswith("apt-get ") for call in self._calls()))
        self.assertFalse((state / "bootstrap-before").exists())

    def test_unknown_or_role_invalid_flags_are_rejected_before_bootstrap(self):
        cases = (
            ("router", "install", ("--unknown",)),
            ("vpn", "install", ("--network-metrics",)),
            ("router", "test", ("--game-metrics",)),
            ("router", "install", ("--purge-packages",)),
            ("vpn", "test", ("--keep-packages",)),
        )
        for role, action, arguments in cases:
            with self.subTest(role=role, action=action, arguments=arguments):
                script = self._script(role=role, action=action)
                result = self._run(script, *arguments)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(arguments[0], result.stderr)
                self.assertFalse(
                    any(call.startswith("apt-get ") for call in self._calls())
                )
                self.assertFalse(self.state_root.exists())
                self.assertFalse(self.lock_file.exists())

    def test_uninstall_package_choices_are_forwarded_to_host_cli(self):
        self._write_python()
        script = self._script(action="uninstall")

        for option in ("--keep-packages", "--purge-packages"):
            with self.subTest(option=option):
                result = self._run(script, option, EXPECT_LOCK_FD="true")

                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(
                    self._calls()[-1],
                    f"python3 {self.bundle / 'host.py'} uninstall {option}",
                )
                self.assertIn("flock -x 9", self._calls())

    def test_bootstrap_failure_keeps_owner_and_package_records(self):
        script = self._script()

        result = self._run(script, APT_UPDATE_FAIL="true", EXPECT_LOCK_FD="true")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Resolve the apt error and rerun install.sh", result.stderr)
        state = self.state_root / "router"
        self.assertEqual(
            (state / "owner").read_text(), "forcad-dependencies-v1:router\n"
        )
        self.assertEqual(
            (state / "bootstrap-before").read_text(),
            "bash\nobsolete-package\npython3-minimal:amd64\nzlib1g:amd64\n",
        )
        self.assertEqual(self._calls()[-1], "apt-get update")
        self.assertFalse((state / "bootstrap-candidates").exists())

    def test_python_install_failure_keeps_snapshot_and_candidate_plan(self):
        script = self._script()

        result = self._run(script, APT_INSTALL_FAIL="true", EXPECT_LOCK_FD="true")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Python bootstrap failed during apt-get install", result.stderr)
        state = self.state_root / "router"
        self.assertEqual(
            (state / "bootstrap-before").read_text(),
            "bash\nobsolete-package\npython3-minimal:amd64\nzlib1g:amd64\n",
        )
        self.assertEqual(
            (state / "bootstrap-candidates").read_text(),
            "libpython3-stdlib:amd64\npython3\npython3-minimal:amd64\n",
        )
        self.assertEqual(
            self._calls()[-1],
            "apt-get install -y --no-install-recommends --no-upgrade --no-remove python3",
        )
        self.assertEqual(
            stat.S_IMODE((state / "bootstrap-candidates").stat().st_mode), 0o600
        )

    def test_previous_package_record_is_preserved(self):
        state = self.state_root / "vpn"
        state.mkdir(parents=True, mode=0o700)
        (state / "owner").write_text("forcad-dependencies-v1:vpn\n")
        (state / "owner").chmod(0o600)
        before = state / "bootstrap-before"
        before.write_text("original-package:amd64\n")
        before.chmod(0o600)
        candidates = state / "bootstrap-candidates"
        candidates.write_text("previous-python-package:amd64\n")
        candidates.chmod(0o600)
        script = self._script(role="vpn")

        result = self._run(
            script,
            "--wireguard-dir",
            "/tmp/server",
            EXPECT_LOCK_FD="true",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(before.read_text(), "original-package:amd64\n")
        self.assertEqual(
            candidates.read_text(),
            "libpython3-stdlib:amd64\nprevious-python-package:amd64\npython3\npython3-minimal:amd64\n",
        )
        self.assertEqual((state / "owner").read_text(), "forcad-dependencies-v1:vpn\n")
        self.assertNotIn("dpkg-query", self._calls())

    def test_unowned_existing_role_state_is_refused(self):
        state = self.state_root / "router"
        state.mkdir(parents=True, mode=0o700)
        script = self._script()

        result = self._run(script, EXPECT_LOCK_FD="true")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("without an owned owner file", result.stderr)
        self.assertFalse(any(call.startswith("apt-get ") for call in self._calls()))

    def test_symlinked_role_state_is_refused(self):
        self.state_root.mkdir()
        target = self.root / "owned-state"
        target.mkdir()
        (self.state_root / "router").symlink_to(target, target_is_directory=True)
        script = self._script()

        result = self._run(script, EXPECT_LOCK_FD="true")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Refusing symlinked dependency state directory", result.stderr)
        self.assertFalse(any(call.startswith("apt-get ") for call in self._calls()))

    def test_bootstrap_only_supports_debian_12_and_13(self):
        for version in ("11", "14"):
            with self.subTest(version=version):
                self.os_release.write_text(f'ID=debian\nVERSION_ID="{version}"\n')
                script = self._script()
                result = self._run(script)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("supports Debian 12 and 13 only", result.stderr)
                self.assertFalse(
                    any(call.startswith("apt-get ") for call in self._calls())
                )
                if self.state_root.exists():
                    shutil.rmtree(self.state_root)
                if self.call_log.exists():
                    self.call_log.unlink()

    def test_debian_13_bootstraps_when_os_release_is_a_symlink(self):
        release = self.root / "release-data"
        release.write_text('ID=debian\nVERSION_ID="13"\n')
        self.os_release.unlink()
        self.os_release.symlink_to(release)
        script = self._script()

        result = self._run(script, EXPECT_LOCK_FD="true")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            [call for call in self._calls() if call.startswith("apt-get ")],
            [
                "apt-get update",
                "apt-get -s install -y --no-install-recommends --no-upgrade --no-remove python3",
                "apt-get install -y --no-install-recommends --no-upgrade --no-remove python3",
            ],
        )

    def test_generated_wrapper_has_valid_bash_syntax(self):
        for role in ("router", "vpn"):
            for action in ("install", "test", "uninstall"):
                script = self._script(role=role, action=action)
                result = subprocess.run(
                    [shutil.which("bash"), "-n", str(script)],
                    check=False,
                    capture_output=True,
                    text=True,
                )
                self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()

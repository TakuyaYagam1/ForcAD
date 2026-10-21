"""Exercise ForcAD Go cache ownership with isolated shell command adapters."""

import hashlib
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from helpers import ROOT

import render


class ForcadCacheTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.target = self.root / "forcad"
        self.log = self.root / "calls"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.global_go = self.root / "global-go"
        for name in ("build", "modules", "path"):
            directory = self.global_go / name
            directory.mkdir(parents=True)
            (directory / "keep").write_text("pre-existing\n")
        self.env = dict(
            os.environ,
            PATH=f"{self.bin}:{Path(shutil.which('bash')).parent}:/usr/bin:/bin",
            CALL_LOG=str(self.log),
            GOCACHE=str(self.global_go / "build"),
            GOMODCACHE=str(self.global_go / "modules"),
            GOPATH=str(self.global_go / "path"),
        )

    def write_executable(self, path, content):
        path.write_text(content)
        path.chmod(0o755)

    def write_fake_commands(self, *, go_version="go1.27.2", download=False):
        scripts = {
            "apt-get": '#!/usr/bin/env bash\nprintf "apt-get %s\\n" "$*" >> "$CALL_LOG"\n',
            "docker": (
                "#!/usr/bin/env bash\n"
                'printf "docker %s\\n" "$*" >> "$CALL_LOG"\n'
                'if [[ "$1" == compose && "$2" == version ]]; then '
                'printf "Docker Compose version v2\\n"; fi\n'
            ),
            "dpkg-query": ('#!/usr/bin/env bash\nprintf "installed test-package\\n"\n'),
            "git": (
                "#!/usr/bin/env bash\n"
                'if [[ "$1" == clone ]]; then\n'
                '    target="${@: -1}"\n'
                '    mkdir -p "$target/config" "$target/deployment/docker"\n'
                "    cat > \"$target/control.sh\" <<'CONTROL'\n"
                "#!/usr/bin/env bash\n"
                'printf "control %s\\n" "$*" >> "$CALL_LOG"\n'
                'printf "goenv %s|%s|%s\\n" "$GOCACHE" "$GOMODCACHE" "$GOPATH" >> "$CALL_LOG"\n'
                'if [[ "$1" == setup ]]; then exit 43; fi\n'
                "CONTROL\n"
                '    chmod 0755 "$target/control.sh"\n'
                "fi\n"
            ),
            "go": f'#!/usr/bin/env bash\nprintf "go version {go_version} linux/amd64\\n"\n',
            "make": "#!/usr/bin/env bash\nexit 0\n",
            "python3": "#!/usr/bin/env bash\nexit 0\n",
            "systemctl": (
                "#!/usr/bin/env bash\n"
                'printf "systemctl %s\\n" "$*" >> "$CALL_LOG"\n'
                'case "$1" in\n'
                '  is-active) printf "inactive\\n" ;;\n'
                '  is-enabled) printf "disabled\\n" ;;\n'
                "esac\n"
            ),
        }
        if download:
            scripts["curl"] = (
                "#!/usr/bin/env bash\n"
                "output=\n"
                "while (($#)); do\n"
                '  if [[ "$1" == -o ]]; then shift; output="$1"; fi\n'
                "  shift\n"
                "done\n"
                'printf "%s\\n" "$output" > "$DOWNLOAD_PATH_LOG"\n'
                'printf "bad archive\\n" > "$output"\n'
            )
            scripts["dpkg"] = (
                "#!/usr/bin/env bash\n"
                'if [[ "$1" == --print-architecture ]]; then printf "amd64\\n"; fi\n'
            )
        for name, content in scripts.items():
            self.write_executable(self.bin / name, content)

    def install_fixture(self):
        config = render.load_config(ROOT / "settings.example.toml")
        config["forcad"]["install_path"] = str(self.target)
        config["env"]["monitoring"] = False
        bundle = self.root / "install-bundle"
        bundle.mkdir()
        (bundle / "config.yml").write_text(render.forcad_config(config))
        (bundle / "environment.env").write_text(render.forcad_environment(config))
        source = render.fresh_forcad_bundle(config)["forcad/install.sh"]
        source = source.replace(
            'if [[ "$EUID" -ne 0 ]]; then\n    exec sudo -- bash "$0" "$@"\nfi\n',
            "",
        )
        source = source.replace(". /etc/os-release", '. "$FAKE_OS_RELEASE"')
        self.script = bundle / "install.sh"
        self.script.write_text(source)
        self.env["FAKE_OS_RELEASE"] = str(self.root / "os-release")
        (self.root / "os-release").write_text(
            "ID=debian\nVERSION_ID=13\nVERSION_CODENAME=trixie\n"
        )
        return bundle

    def uninstall_fixture(self, monitoring=True):
        config = render.load_config(ROOT / "settings.example.toml")
        config["forcad"]["install_path"] = str(self.target)
        config["env"]["monitoring"] = monitoring
        self.target.mkdir()
        self.marker = self.target / ".forcad-fresh-install"
        repository = config["forcad"]["repository_url"]
        revision = config["forcad"]["revision"]
        self.marker.write_text(f"{repository} {revision}\n")
        self.record("managed-packages", "docker-ce\ncontainerd.io\n", "packages")
        (self.target / ".forcad-docker-state").write_text("active\nenabled\n")
        self.record("monitoring", f"{str(monitoring).lower()}\n")

        bundle = self.root / "uninstall-bundle"
        (bundle / "host").mkdir(parents=True)
        (bundle / "host/uninstall.sh").write_text(
            'printf "host %s\\n" "$*" >> "$CALL_LOG"\n'
        )
        source = render.fresh_forcad_bundle(config)["forcad/uninstall.sh"]
        source = source.replace(
            'if [[ "$EUID" -ne 0 ]]; then exec sudo -- bash "$0" "$@"; fi\n',
            "",
        )
        self.script = bundle / "uninstall.sh"
        self.script.write_text(source)

        self.write_executable(
            self.target / "control.sh",
            "#!/usr/bin/env bash\n"
            'printf "control %s\\n" "$*" >> "$CALL_LOG"\n'
            'printf "goenv %s|%s|%s\\n" "$GOCACHE" "$GOMODCACHE" "$GOPATH" >> "$CALL_LOG"\n'
            'printf "path %s\\n" "$PATH" >> "$CALL_LOG"\n',
        )
        self.write_executable(
            self.bin / "docker",
            '#!/usr/bin/env bash\nprintf "docker %s\\n" "$*" >> "$CALL_LOG"\n',
        )
        self.write_executable(
            self.bin / "systemctl",
            "#!/usr/bin/env bash\n"
            'printf "systemctl %s\\n" "$*" >> "$CALL_LOG"\n'
            'case "$1" in\n'
            '  is-active) printf "inactive\\n" ;;\n'
            '  is-enabled) printf "disabled\\n" ;;\n'
            "esac\n",
        )
        self.write_executable(
            self.bin / "apt-get",
            '#!/usr/bin/env bash\nprintf "apt-get %s\\n" "$*" >> "$CALL_LOG"\n',
        )
        self.write_executable(
            self.bin / "sysctl",
            '#!/usr/bin/env bash\nprintf "sysctl %s\\n" "$*" >> "$CALL_LOG"\n',
        )
        return bundle

    def record(self, name, content, key=None):
        (self.target / f".forcad-{name}").write_text(content)
        digest = hashlib.sha256(content.encode()).hexdigest()
        with self.marker.open("a") as stream:
            stream.write(f"{key or name}_sha256={digest}\n")

    def run_script(self, *arguments, input_text=""):
        return subprocess.run(
            [shutil.which("bash"), str(self.script), *arguments],
            input=input_text,
            capture_output=True,
            text=True,
            env=self.env,
            check=False,
        )

    def assert_global_go_is_intact(self):
        for name in ("build", "modules", "path"):
            self.assertEqual(
                (self.global_go / name / "keep").read_text(), "pre-existing\n"
            )

    def test_install_uses_target_caches_and_preserves_existing_caches(self):
        self.install_fixture()
        self.write_fake_commands()
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Fresh installation failed", result.stderr)
        self.assertFalse(self.target.exists())
        calls = self.log.read_text().splitlines()
        self.assertIn("control setup", calls)
        self.assertIn("control down -v --remove-orphans", calls)
        expected = "|".join(
            str(self.target / ".forcad-go" / name)
            for name in ("build", "modules", "path")
        )
        self.assertEqual(
            [
                line.removeprefix("goenv ")
                for line in calls
                if line.startswith("goenv ")
            ],
            [expected, expected],
        )
        self.assert_global_go_is_intact()
        self.assertTrue((self.bin / "go").is_file())

    def test_failed_go_download_removes_its_temporary_directory(self):
        self.install_fixture()
        self.write_fake_commands(go_version="go1.26.9", download=True)
        self.env["DOWNLOAD_PATH_LOG"] = str(self.root / "download-path")
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Go archive checksum mismatch", result.stderr)
        archive = Path((self.root / "download-path").read_text().strip())
        self.assertFalse(archive.parent.exists())
        self.assertFalse(self.target.exists())
        self.assert_global_go_is_intact()

    def test_uninstall_uses_the_built_binary_for_teardown(self):
        self.uninstall_fixture()
        binary = self.target / "backend/bin/forcad"
        binary.parent.mkdir(parents=True)
        self.write_executable(
            binary,
            '#!/usr/bin/env bash\nprintf "binary %s\\n" "$*" >> "$CALL_LOG"\n',
        )
        result = self.run_script("--confirm")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text().splitlines()
        self.assertIn(
            f"binary control --project-root {self.target} monitoring down -v --remove-orphans",
            calls,
        )
        self.assertFalse(any(line.startswith("control ") for line in calls))
        self.assertFalse(self.target.exists())
        self.assert_global_go_is_intact()

    def test_older_install_uses_scoped_caches_and_toolchain_path(self):
        self.uninstall_fixture()
        go_bin = self.target / ".toolchain/go1.27.2/bin"
        go_bin.mkdir(parents=True)
        self.write_executable(go_bin / "go", "#!/usr/bin/env bash\nexit 0\n")
        result = self.run_script("--confirm")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text().splitlines()
        self.assertIn("control monitoring down -v --remove-orphans", calls)
        expected = "|".join(
            str(self.target / ".forcad-go" / name)
            for name in ("build", "modules", "path")
        )
        self.assertEqual(
            [
                line.removeprefix("goenv ")
                for line in calls
                if line.startswith("goenv ")
            ],
            [expected],
        )
        path_line = next(line for line in calls if line.startswith("path "))
        self.assertTrue(path_line.startswith(f"path {go_bin}:"))
        self.assertFalse(self.target.exists())
        self.assert_global_go_is_intact()

    def test_older_install_refuses_a_cache_symlink(self):
        self.uninstall_fixture()
        self.global_go.mkdir(exist_ok=True)
        (self.target / ".forcad-go").symlink_to(
            self.global_go, target_is_directory=True
        )
        result = self.run_script("--confirm")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlinked Go cache path", result.stderr)
        self.assertFalse(
            any(
                line.startswith("control ")
                for line in self.log.read_text().splitlines()
            )
        )
        self.assertTrue(self.target.exists())
        self.assert_global_go_is_intact()


if __name__ == "__main__":
    unittest.main()

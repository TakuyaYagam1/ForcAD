"""Exercise the generated shell entry point with isolated command adapters."""

import hashlib
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from helpers import ROOT

import render


class ForcadUninstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.target = self.root / "forcad"
        self.target.mkdir()
        self.marker = self.target / ".forcad-fresh-install"
        self.marker.write_text(
            "https://github.com/TakuyaYagam1/ForcAD.git old-branch\n"
        )
        self.record("managed-packages", "docker-ce\ncontainerd.io\n", key="packages")
        (self.target / ".forcad-docker-state").write_text("active\nenabled\n")
        self.record("monitoring", "true\n")
        for name in ("build", "modules", "path"):
            (self.target / ".forcad-go" / name).mkdir(parents=True, exist_ok=True)
        go_bin = self.target / ".toolchain/go1.27.2/bin"
        go_bin.mkdir(parents=True)
        (go_bin / "go").write_text("fixture toolchain\n")
        self.log = self.root / "calls"
        self.bundle = self.root / "bundle"
        (self.bundle / "host").mkdir(parents=True)
        (self.bundle / "host/uninstall.sh").write_text(
            'printf "host %s\\n" "$*" >> "$CALL_LOG"\n'
        )
        control = self.target / "control.sh"
        control.write_text(
            '#!/usr/bin/env bash\nprintf "control %s\\n" "$*" >> "$CALL_LOG"\n'
        )
        control.chmod(0o755)
        self.fake_bin = self.root / "bin"
        self.fake_bin.mkdir()
        for name in (
            "apt-get",
            "systemctl",
            "docker",
            "dockerd",
            "containerd",
            "ctr",
            "sysctl",
        ):
            script = self.fake_bin / name
            script.write_text(
                "#!/usr/bin/env bash\n"
                f'printf "{name} %s\\n" "$*" >> "$CALL_LOG"\n'
                + {
                    "apt-get": 'if [[ "$1" == "-s" ]]; then printf "%s" "${APT_SIMULATION:-}"; fi\n',
                    "systemctl": (
                        'if [[ "$1" == "is-active" && "${SYSTEMD_INACTIVE:-false}" == true ]]; then exit 3; fi\n'
                        'if [[ "$1" == "show" ]]; then printf "%s\\n" "${SYSTEMD_LOAD_STATE:-loaded}"; exit 0; fi\n'
                    ),
                    "docker": (
                        'case "$*" in\n'
                        '    "image ls -aq") printf "%s" "${DOCKER_IMAGE_OUTPUT:-}" ;;\n'
                        '    "buildx du --builder default --format {{.ID}}") printf "%s" "${BUILDX_CACHE_OUTPUT:-}" ;;\n'
                        "esac\n"
                    ),
                    "ctr": (
                        'case "$*" in\n'
                        '    "--address /run/containerd/containerd.sock namespaces list --quiet") printf "%s" "${CTR_NAMESPACES:-}" ;;\n'
                        '    *) printf "%s" "${CTR_OBJECTS:-}" ;;\n'
                        "esac\n"
                    ),
                }.get(name, "")
            )
            script.chmod(0o755)
        (self.fake_bin / "forcad-test-dockerd").symlink_to("dockerd")
        (self.fake_bin / "forcad-test-containerd").symlink_to("containerd")
        python = self.fake_bin / "python3"
        python.write_text(
            "#!/usr/bin/env bash\n"
            'printf "python3 %s\\n" "$*" >> "$CALL_LOG"\n'
            'printf "python-env BUILDX_CONFIG=%s BUILDX_BUILDER=%s\\n" '
            '"${BUILDX_CONFIG:-}" "${BUILDX_BUILDER:-}" >> "$CALL_LOG"\n'
            'case "$*" in\n'
            "    \"-m hostlib.docker_state capture\") printf '{}\\n' ;;\n"
            '    "-m hostlib.docker_state restore "*) ;;\n'
            '    "-m hostlib.docker_resources cleanup "*) exit "${PYTHON_EXIT:-0}" ;;\n'
            "esac\n"
        )
        python.chmod(0o755)
        self.env = dict(
            os.environ,
            PATH=f"{self.fake_bin}:{os.environ['PATH']}",
            CALL_LOG=str(self.log),
        )
        config = render.load_config(ROOT / "settings.example.toml")
        config["forcad"]["install_path"] = str(self.target)
        source = render.fresh_forcad_bundle(config)["forcad/uninstall.sh"]
        # Use fixture-only daemon probes so an installed host daemon cannot
        # satisfy tool discovery after a simulated partial package removal.
        source = source.replace(
            "command -v dockerd >/dev/null",
            "command -v forcad-test-dockerd >/dev/null",
        ).replace(
            "command -v containerd >/dev/null",
            "command -v forcad-test-containerd >/dev/null",
        )
        source = source.replace(
            'if [[ "$EUID" -ne 0 ]]; then exec sudo -- bash "$0" "$@"; fi\n', ""
        )
        self.script = self.bundle / "uninstall.sh"
        self.script.write_text(source)

    def record(self, name, content, key=None):
        (self.target / f".forcad-{name}").write_text(content)
        digest = hashlib.sha256(content.encode()).hexdigest()
        with self.marker.open("a") as stream:
            stream.write(f"{key or name}_sha256={digest}\n")

    def replace_packages(self, content):
        package_file = self.target / ".forcad-managed-packages"
        package_file.write_text(content)
        digest = hashlib.sha256(content.encode()).hexdigest()
        lines = [
            line
            for line in self.marker.read_text().splitlines()
            if not line.startswith("packages_sha256=")
        ]
        lines.append(f"packages_sha256={digest}")
        self.marker.write_text("\n".join(lines) + "\n")

    def add_resource_snapshots(self):
        self.record("resources-before", "{}\n")
        self.record("resources-applied", "{}\n")

    def remove_daemon_probes(self):
        (self.fake_bin / "forcad-test-dockerd").unlink()
        (self.fake_bin / "forcad-test-containerd").unlink()

    def assert_owned_go_removed(self):
        self.assertFalse((self.target / ".forcad-go").exists())
        self.assertFalse((self.target / ".toolchain").exists())

    def assert_resource_runtime_restored(self, calls, cycles=1):
        capture_call = "python3 -m hostlib.docker_state capture\n"
        captures = [
            position
            for position in range(len(calls))
            if calls.startswith(capture_call, position)
        ]
        cleanups = [
            position
            for position in range(len(calls))
            if calls.startswith(
                "python3 -m hostlib.docker_resources cleanup ", position
            )
        ]
        restores = [
            position
            for position in range(len(calls))
            if calls.startswith("python3 -m hostlib.docker_state restore ", position)
        ]
        self.assertEqual(len(captures), cycles * 2)
        self.assertEqual(len(cleanups), cycles)
        self.assertEqual(len(restores), cycles)
        for cycle in range(cycles):
            baseline, temporary = captures[cycle * 2 : cycle * 2 + 2]
            cleanup = cleanups[cycle]
            temporary_start = calls.rindex(
                "systemctl start docker.service\n", 0, cleanup
            )
            self.assertLess(baseline, temporary_start)
            self.assertLess(temporary_start, cleanup)
            self.assertLess(cleanup, temporary)
            self.assertLess(temporary, restores[cycle])

    def uninstall(self, answer="", *options):
        return subprocess.run(
            ["bash", str(self.script), "--confirm", *options],
            input=answer,
            capture_output=True,
            text=True,
            env=self.env,
            check=False,
        )

    def test_yes_stops_monitoring_and_services_before_purging_packages(self):
        self.add_resource_snapshots()
        self.record(
            "sysctl", "net.ipv4.ip_forward = 0\nnet.ipv6.conf.all.forwarding = 0\n"
        )
        result = self.uninstall("yes\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        self.assertIn("control monitoring down -v --remove-orphans", calls)
        self.assertIn("host --force\n", calls)
        self.assertIn("host --discard-backups", calls)
        for unit in ("docker.service", "docker.socket", "containerd.service"):
            self.assertIn(f"systemctl stop {unit}", calls)
            self.assertIn(f"systemctl disable {unit}", calls)
        self.assertIn("sysctl -w net.ipv4.ip_forward=0", calls)
        cleanup = "python3 -m hostlib.docker_resources cleanup "
        self.assertIn(cleanup, calls)
        self.assertIn(".forcad-resources-applied --exclusive", calls)
        self.assertIn(
            f"python-env BUILDX_CONFIG={self.target}/.forcad-buildx "
            "BUILDX_BUILDER=default",
            calls,
        )
        self.assert_resource_runtime_restored(calls)
        self.assertLess(
            calls.index("systemctl stop docker.service"), calls.index("apt-get purge")
        )
        self.assertLess(calls.index(cleanup), calls.index("apt-get -s purge"))
        self.assertFalse(self.target.exists())
        self.assert_owned_go_removed()

    def test_no_removes_deployment_and_stops_new_docker_but_keeps_packages(self):
        self.add_resource_snapshots()
        result = self.uninstall("n\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        self.assertIn("systemctl stop docker.service", calls)
        self.assertNotIn("apt-get", calls)
        self.assertNotIn("hostlib.docker_resources cleanup", calls)
        self.assertFalse(self.target.exists())
        self.assert_owned_go_removed()

    def test_eof_keeps_packages_and_repeated_uninstall_is_a_noop(self):
        result = self.uninstall()
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        self.assertNotIn("apt-get", calls)
        result = self.uninstall()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.log.read_text(), calls)

    def test_package_cascade_into_preexisting_package_is_refused(self):
        self.add_resource_snapshots()
        self.env["APT_SIMULATION"] = "Purg docker-ce [1]\nPurg operator-package [1]\n"
        result = self.uninstall("", "--purge-packages")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("pre-existing package: operator-package", result.stderr)
        self.assertNotIn("apt-get purge -y", self.log.read_text())
        self.assertTrue(self.target.exists(), "retain records for retry")
        receipt = self.target / ".forcad-resources-cleaned"
        self.assertEqual(receipt.read_text(), "true\n")
        digest = hashlib.sha256(receipt.read_bytes()).hexdigest()
        self.assertIn(f"resources-cleaned_sha256={digest}", self.marker.read_text())
        self.assertEqual(
            self.log.read_text().count("python3 -m hostlib.docker_resources cleanup "),
            1,
        )
        self.assertEqual(self.marker.read_text().count("resources-cleaned_sha256="), 1)
        result = self.uninstall("", "--purge-packages")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("pre-existing package: operator-package", result.stderr)
        self.assertTrue(self.target.exists(), "retain records for another retry")
        calls = self.log.read_text()
        self.assertEqual(calls.count("python3 -m hostlib.docker_resources cleanup "), 2)
        self.assert_resource_runtime_restored(calls, cycles=2)
        self.assertEqual(self.marker.read_text().count("resources-cleaned_sha256="), 1)
        before_keep = calls
        result = self.uninstall("", "--keep-packages")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.log.read_text(), before_keep)
        self.assertFalse(self.target.exists())
        self.assert_owned_go_removed()

    def test_empty_package_record_still_cleans_resources_without_apt(self):
        self.replace_packages("")
        self.record("docker-state", "inactive\ndisabled\n")
        self.add_resource_snapshots()
        result = self.uninstall("", "--purge-packages")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        self.assertIn("python3 -m hostlib.docker_resources cleanup ", calls)
        self.assertNotIn("apt-get", calls)
        self.assert_resource_runtime_restored(calls)
        self.assertFalse(self.target.exists())
        self.assert_owned_go_removed()

    def test_resource_cleanup_failure_preserves_target_and_restores_services(self):
        self.add_resource_snapshots()
        self.env["PYTHON_EXIT"] = "17"
        result = self.uninstall("", "--purge-packages")
        self.assertEqual(result.returncode, 17, result.stderr)
        calls = self.log.read_text()
        self.assertIn("python3 -m hostlib.docker_resources cleanup ", calls)
        self.assert_resource_runtime_restored(calls)
        self.assertIn("systemctl start docker.service", calls)
        self.assertIn("systemctl stop docker.service", calls)
        self.assertIn("systemctl disable docker.service", calls)
        self.assertNotIn("apt-get", calls)
        self.assertTrue(self.target.exists())
        self.assertFalse((self.target / ".forcad-resources-cleaned").exists())

    def test_partial_apt_removal_skips_cleanup_when_daemons_are_missing_and_inactive(
        self,
    ):
        self.add_resource_snapshots()
        self.env["APT_SIMULATION"] = "Purg docker-ce [1]\nPurg operator-package [1]\n"
        result = self.uninstall("", "--purge-packages")
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.target / ".forcad-resources-cleaned").exists())
        calls_before_retry = self.log.read_text()

        self.remove_daemon_probes()
        self.env.pop("APT_SIMULATION")
        self.env["SYSTEMD_INACTIVE"] = "true"
        self.env["SYSTEMD_LOAD_STATE"] = "not-found"
        result = self.uninstall("", "--purge-packages")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        retry_calls = calls[len(calls_before_retry) :]
        self.assertIn("systemctl is-active --quiet docker.service", retry_calls)
        self.assertIn("systemctl is-active --quiet containerd.service", retry_calls)
        for unit in ("docker.socket", "docker.service", "containerd.service"):
            self.assertIn(f"systemctl show {unit} -p LoadState --value", retry_calls)
            self.assertNotIn(f"systemctl stop {unit}", retry_calls)
        self.assertNotIn("python3 -m hostlib.docker_resources cleanup", retry_calls)
        self.assertNotIn("python3 -m hostlib.docker_state capture", retry_calls)
        self.assertNotIn("docker buildx version", retry_calls)
        self.assertIn("apt-get purge -y", retry_calls)
        self.assertEqual(calls.count("python3 -m hostlib.docker_resources cleanup "), 1)
        self.assertFalse(self.target.exists())

    def test_missing_cleanup_tools_without_receipt_keeps_installation(self):
        self.add_resource_snapshots()
        self.remove_daemon_probes()
        result = self.uninstall("", "--purge-packages")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Docker cleanup tools are missing", result.stderr)
        calls = self.log.read_text()
        self.assertNotIn("python3 -m hostlib.docker_resources cleanup", calls)
        self.assertNotIn("apt-get", calls)
        self.assertTrue(self.target.exists())
        self.assertFalse((self.target / ".forcad-resources-cleaned").exists())

    def test_legacy_install_with_empty_images_cache_and_containerd_restores_runtime(
        self,
    ):
        result = self.uninstall("", "--purge-packages")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        self.assertIn("docker image ls -aq", calls)
        self.assertIn("docker buildx du --builder default --format {{.ID}}", calls)
        self.assertIn(
            "ctr --address /run/containerd/containerd.sock namespaces list --quiet",
            calls,
        )
        captures = [
            position
            for position in range(len(calls))
            if calls.startswith("python3 -m hostlib.docker_state capture\n", position)
        ]
        restore = calls.index("python3 -m hostlib.docker_state restore ")
        image_list = calls.index("docker image ls -aq")
        cache_list = calls.index("docker buildx du --builder default --format {{.ID}}")
        namespace_list = calls.index(
            "ctr --address /run/containerd/containerd.sock namespaces list --quiet"
        )
        temporary_start = calls.rindex(
            "systemctl start docker.service\n", 0, image_list
        )
        self.assertEqual(len(captures), 2)
        self.assertLess(captures[0], temporary_start)
        self.assertLess(temporary_start, image_list)
        self.assertLess(image_list, cache_list)
        self.assertLess(cache_list, namespace_list)
        self.assertLess(namespace_list, captures[1])
        self.assertLess(captures[1], restore)
        self.assertNotIn("hostlib.docker_resources cleanup", calls)
        self.assertFalse(self.target.exists())

    def test_legacy_install_refuses_existing_images_or_build_cache(self):
        self.env["DOCKER_IMAGE_OUTPUT"] = "existing-image\n"
        result = self.uninstall("", "--purge-packages")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no image/cache ownership record", result.stderr)
        self.assertTrue(self.target.exists())
        self.assertNotIn("apt-get", self.log.read_text())

        self.env["DOCKER_IMAGE_OUTPUT"] = ""
        self.env["BUILDX_CACHE_OUTPUT"] = "existing-cache\n"
        result = self.uninstall("", "--purge-packages")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no image/cache ownership record", result.stderr)
        self.assertTrue(self.target.exists())
        self.assertNotIn("apt-get", self.log.read_text())
        self.assertFalse((self.target / ".forcad-resources-cleaned").exists())
        calls = self.log.read_text()
        self.assertEqual(calls.count("docker image ls -aq\n"), 2)
        self.assertEqual(
            calls.count("docker buildx du --builder default --format {{.ID}}\n"),
            2,
        )
        self.assertEqual(calls.count("python3 -m hostlib.docker_state capture\n"), 4)
        self.assertEqual(calls.count("python3 -m hostlib.docker_state restore "), 2)

    def test_unknown_containerd_namespace_preserves_target_and_skips_apt(self):
        self.add_resource_snapshots()
        self.env["CTR_NAMESPACES"] = "other-team\n"
        result = self.uninstall("", "--purge-packages")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Other containerd namespace exists: other-team", result.stderr)
        calls = self.log.read_text()
        self.assertIn(
            "ctr --address /run/containerd/containerd.sock namespaces list --quiet",
            calls,
        )
        self.assertNotIn("apt-get", calls)
        self.assert_resource_runtime_restored(calls)
        self.assertFalse((self.target / ".forcad-resources-cleaned").exists())
        self.assertTrue(self.target.exists())

    def test_corrupt_directory_record_aborts_before_any_mutation(self):
        self.record("created-dirs", "")
        (self.target / ".forcad-created-dirs").write_text("/home\n")
        result = self.uninstall("yes\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum mismatch", result.stderr)
        self.assertFalse(self.log.exists())

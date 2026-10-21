import hashlib
import io
import json
import os
import subprocess
import tarfile
from contextlib import contextmanager, redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import TestCase
from unittest.mock import patch

import scan_images

IMAGE_ID = "sha256:" + "a" * 64
CONFIG = b'{"architecture":"amd64"}'
CONFIG_ID = "sha256:" + hashlib.sha256(CONFIG).hexdigest()


class FakeDocker:
    def __init__(self):
        self.calls = []
        self.tags = {}
        self.build_status = {}
        self.scan_status = 0
        self.convert_status = 0
        self.report_mode = "valid"
        self.report_id = CONFIG_ID
        self.security_options = ["name=seccomp,profile=builtin", "name=cgroupns"]
        self.last_target = ""
        self.last_archive = None

    def __call__(self, command, **kwargs):
        args = command[1:]
        self.calls.append(args)
        rc, stdout = 0, ""
        if args[0] == "info":
            stdout = json.dumps(self.security_options)
        elif args[:2] == ["image", "ls"]:
            tag = args[-1].split("=", 1)[1]
            stdout = self.tags.get(tag, "")
        elif args[0] == "build":
            tag = args[args.index("--tag") + 1]
            self.last_target = tag.split(":", 1)[1]
            iidfile = Path(args[args.index("--iidfile") + 1])
            iidfile.write_text(IMAGE_ID + "\n", encoding="ascii")
            self.tags[tag] = IMAGE_ID
            rc = self.build_status.get(self.last_target, 0)
        elif args[:2] == ["image", "inspect"]:
            tag = args[-1]
            if tag not in self.tags:
                rc = 1
            else:
                stdout = self.tags[tag] + "\n"
        elif args[:2] == ["image", "save"]:
            self.last_archive = Path(args[args.index("--output") + 1])
            config_name = f"{hashlib.sha256(CONFIG).hexdigest()}.json"
            manifest = json.dumps(
                [{"Config": config_name, "RepoTags": [], "Layers": []}]
            ).encode()
            with tarfile.open(self.last_archive, "w") as image:
                manifest_info = tarfile.TarInfo("manifest.json")
                manifest_info.size = len(manifest)
                image.addfile(manifest_info, io.BytesIO(manifest))
                config_info = tarfile.TarInfo(config_name)
                config_info.size = len(CONFIG)
                image.addfile(config_info, io.BytesIO(CONFIG))
        elif args[0] == "run":
            trivy_index = args.index(scan_images.SCANNER_IMAGE)
            subcommand = args[trivy_index + 1]
            if subcommand == "image":
                report_arg = args[args.index("--output") + 1]
                target = report_arg.split("/")[-2]
                report = (
                    self._mounted_path(args, "/reports")
                    / "images"
                    / target
                    / "trivy-image.json"
                )
                if self.report_mode != "missing":
                    if self.report_mode == "malformed-object":
                        payload = {}
                    elif self.report_mode == "malformed-list":
                        payload = []
                    elif self.report_mode == "wrong-type":
                        payload = self._document()
                        payload["ArtifactType"] = "filesystem"
                    elif self.report_mode == "wrong-id":
                        payload = self._document()
                        payload["Metadata"]["ImageID"] = "sha256:" + "b" * 64
                    else:
                        payload = self._document()
                    report.write_text(json.dumps(payload), encoding="utf-8")
                rc = self.scan_status
            elif subcommand == "convert":
                table_arg = args[args.index("--output") + 1]
                target = table_arg.split("/")[-2]
                table = (
                    self._mounted_path(args, "/reports")
                    / "images"
                    / target
                    / "table.txt"
                )
                table.write_text(f"summary for {target}\n", encoding="utf-8")
                rc = self.convert_status
        elif args[:2] == ["image", "rm"]:
            self.tags.pop(args[-1], None)
        return subprocess.CompletedProcess(command, rc, stdout=stdout, stderr="")

    @staticmethod
    def _mounted_path(args, target):
        mount_index = args.index("--mount", args.index("--tmpfs") + 2)
        while mount_index < len(args):
            spec = args[mount_index + 1]
            if f"target={target}" in spec:
                return Path(spec.split("source=", 1)[1].split(",target=", 1)[0])
            try:
                mount_index = args.index("--mount", mount_index + 1)
            except ValueError:
                break
        raise AssertionError(f"missing {target} mount")

    def _document(self):
        return {
            "ArtifactType": "container_image",
            "Results": [{"Target": "alpine"}],
            "Metadata": {"ImageID": self.report_id},
        }


@contextmanager
def no_lock(_target):
    yield


class ImageScanTest(TestCase):
    def setUp(self):
        self.temp = TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.reports = self.base / "reports"
        self.cache = self.base / "cache"
        self.reports.mkdir()
        self.cache.mkdir()
        self.docker = FakeDocker()
        self.run_patch = patch.object(
            scan_images.subprocess, "run", side_effect=self.docker
        )
        self.run_patch.start()
        self.addCleanup(self.run_patch.stop)
        self.lock_patch = patch.object(scan_images, "target_lock", no_lock)
        self.lock_patch.start()
        self.addCleanup(self.lock_patch.stop)

    def call_target(self, target="runtime", rootless=False):
        output = StringIO()
        with redirect_stdout(output), redirect_stderr(StringIO()):
            status = scan_images.scan_target(
                target, self.reports, self.cache, "docker", rootless
            )
        return status, output.getvalue()

    def test_no_targets_selects_all_five(self):
        args = scan_images.build_parser().parse_args(
            ["--reports", str(self.reports), "--cache", str(self.cache)]
        )
        self.assertEqual(args.targets, [])
        selected = []
        with patch.object(
            scan_images,
            "scan_target",
            side_effect=lambda target, *_: selected.append(target) or 0,
        ):
            self.assertEqual(
                scan_images.run_targets(
                    args.targets, self.reports, self.cache, "docker"
                ),
                0,
            )
        self.assertEqual(
            selected, ["runtime", "worker", "nginx", "postgres", "pgbouncer"]
        )

    def test_build_scan_and_cleanup_use_image_id_and_pinned_scanner(self):
        status, table = self.call_target()
        self.assertEqual(status, 0)
        self.assertIn("summary for runtime", table)
        build = next(call for call in self.docker.calls if call[0] == "build")
        self.assertIn("--pull", build)
        self.assertEqual(build[build.index("--file") + 1], "backend/Dockerfile")
        self.assertEqual(build[build.index("--target") + 1], "runtime")
        saved = next(
            call for call in self.docker.calls if call[:2] == ["image", "save"]
        )
        self.assertEqual(saved[-1], IMAGE_ID)
        scan = next(
            call
            for call in self.docker.calls
            if call[0] == "run"
            and call[call.index(scan_images.SCANNER_IMAGE) + 1] == "image"
        )
        self.assertIn("--read-only", scan)
        self.assertIn("HOME=/tmp", scan)
        self.assertIn("--input", scan)
        self.assertEqual(scan[scan.index("--user") + 1], f"{os.getuid()}:{os.getgid()}")
        self.assertNotIn("/var/run/docker.sock", scan)
        self.assertIn("security/trivy/image.yml", scan[scan.index("--config") + 1])
        cleanup = next(
            call
            for call in self.docker.calls
            if call[:3] == ["image", "rm", "--no-prune"]
        )
        self.assertEqual(cleanup[-1], "forcad-security:runtime")
        self.assertFalse(
            any(
                call[:2] == ["image", "rm"] and IMAGE_ID in call
                for call in self.docker.calls
            )
        )
        output_dir = self.reports / "images" / "runtime"
        self.assertEqual((output_dir / "image-id.txt").read_text().strip(), IMAGE_ID)
        self.assertEqual((output_dir / "image-id.txt").stat().st_mode & 0o777, 0o600)
        self.assertEqual(
            (output_dir / "trivy-image.json").stat().st_mode & 0o777, 0o600
        )
        self.assertEqual((output_dir / "table.txt").stat().st_mode & 0o777, 0o600)
        self.assertFalse(self.docker.last_archive.exists())

    def test_scanner_finding_status_is_preserved_and_table_is_still_written(self):
        self.docker.scan_status = 1
        status, table = self.call_target()
        self.assertEqual(status, 1)
        self.assertIn("summary for runtime", table)
        self.assertTrue((self.reports / "images/runtime/table.txt").is_file())

    def test_missing_and_malformed_reports_fail_and_are_not_converted(self):
        for mode in ("missing", "malformed-object", "malformed-list"):
            with self.subTest(mode=mode):
                self.docker = FakeDocker()
                self.docker.report_mode = mode
                self.run_patch.stop()
                self.run_patch = patch.object(
                    scan_images.subprocess, "run", side_effect=self.docker
                )
                self.run_patch.start()
                status, _ = self.call_target()
                self.assertEqual(status, 1)
                self.assertFalse(
                    any(
                        call[0] == "run"
                        and call[call.index(scan_images.SCANNER_IMAGE) + 1] == "convert"
                        for call in self.docker.calls
                    )
                )
                report = self.reports / "images/runtime/trivy-image.json"
                if mode == "missing":
                    self.assertFalse(report.exists())
                else:
                    self.assertTrue(report.exists())

    def test_report_type_and_image_id_must_match(self):
        for mode in ("wrong-type", "wrong-id"):
            with self.subTest(mode=mode):
                self.docker = FakeDocker()
                self.docker.report_mode = mode
                self.run_patch.stop()
                self.run_patch = patch.object(
                    scan_images.subprocess, "run", side_effect=self.docker
                )
                self.run_patch.start()
                status, _ = self.call_target()
                self.assertEqual(status, 1)

    def test_preexisting_tag_is_neither_built_nor_removed(self):
        self.docker.tags["forcad-security:runtime"] = IMAGE_ID
        status, _ = self.call_target()
        self.assertEqual(status, 1)
        self.assertFalse(any(call[0] == "build" for call in self.docker.calls))
        self.assertFalse(any(call[:2] == ["image", "rm"] for call in self.docker.calls))

    def test_build_failure_does_not_stop_later_target(self):
        self.docker.build_status["runtime"] = 17
        with redirect_stdout(StringIO()), redirect_stderr(StringIO()):
            status = scan_images.run_targets(
                ["runtime", "worker"], self.reports, self.cache, "docker"
            )
        self.assertEqual(status, 17)
        built = [
            call[call.index("--tag") + 1]
            for call in self.docker.calls
            if call[0] == "build"
        ]
        self.assertEqual(built, ["forcad-security:runtime", "forcad-security:worker"])
        self.assertTrue(any(call[0] == "run" for call in self.docker.calls))

    def test_target_lock_rejects_concurrent_scan(self):
        self.lock_patch.stop()
        lock_root = self.base / "checkout"
        (lock_root / "security").mkdir(parents=True)
        with (
            patch.object(scan_images, "ROOT", lock_root),
            scan_images.target_lock("runtime"),
            self.assertRaises(RuntimeError),
            scan_images.target_lock("runtime"),
        ):
            pass

    def test_rootless_docker_runs_scanner_as_container_root(self):
        self.docker.security_options.append("name=rootless")
        self.assertTrue(scan_images.docker_is_rootless("docker"))
        status, _ = self.call_target(rootless=True)
        self.assertEqual(status, 0)
        scan = next(
            call
            for call in self.docker.calls
            if call[0] == "run"
            and call[call.index(scan_images.SCANNER_IMAGE) + 1] == "image"
        )
        self.assertEqual(scan[scan.index("--user") + 1], "0:0")

    def test_unreadable_docker_mode_does_not_assume_rootful(self):
        self.docker.security_options = {"name": "rootless"}
        with self.assertRaises(RuntimeError):
            scan_images.docker_is_rootless("docker")

    def test_cleanup_daemon_error_is_not_treated_as_success(self):
        inspect = subprocess.CompletedProcess(
            [], 1, stdout="", stderr="daemon unavailable"
        )
        listing = subprocess.CompletedProcess(
            [], 1, stdout="", stderr="daemon unavailable"
        )
        with (
            patch.object(scan_images, "docker_run", side_effect=[inspect, listing]),
            redirect_stderr(StringIO()),
        ):
            self.assertEqual(
                scan_images.cleanup_image(
                    "runtime", "forcad-security:runtime", IMAGE_ID, "docker"
                ),
                1,
            )

    def test_cleanup_accepts_confirmed_absent_tag(self):
        inspect = subprocess.CompletedProcess([], 1, stdout="", stderr="no such image")
        listing = subprocess.CompletedProcess([], 0, stdout="", stderr="")
        with patch.object(scan_images, "docker_run", side_effect=[inspect, listing]):
            self.assertEqual(
                scan_images.cleanup_image(
                    "runtime", "forcad-security:runtime", IMAGE_ID, "docker"
                ),
                0,
            )

    def test_export_timeout_cleans_only_the_build_tag(self):
        original_run = self.docker

        def timeout_save(command, **kwargs):
            args = command[1:]
            if args[:2] == ["image", "save"]:
                self.docker.calls.append(args)
                raise subprocess.TimeoutExpired(command, timeout=1)
            return original_run(command, **kwargs)

        self.run_patch.stop()
        self.run_patch = patch.object(
            scan_images.subprocess, "run", side_effect=timeout_save
        )
        self.run_patch.start()
        status, _ = self.call_target()
        self.assertEqual(status, 124)
        self.assertNotIn("forcad-security:runtime", self.docker.tags)
        cleanup = [
            call
            for call in self.docker.calls
            if call[:3] == ["image", "rm", "--no-prune"]
        ]
        self.assertEqual(len(cleanup), 1)
        self.assertEqual(cleanup[0][-1], "forcad-security:runtime")
        self.assertFalse(any(IMAGE_ID in call for call in cleanup))

#!/usr/bin/env python3
"""Build and scan the production container images with pinned Trivy."""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
from collections.abc import Iterator, Sequence
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parents[1]
SCANNER_IMAGE = (
    "ghcr.io/aquasecurity/trivy:0.75.0@"
    "sha256:9db099105405c648166e6b94155eb32f8da12673cf1f455207f7385cc9a77283"
)
IMAGE_PREFIX = "forcad-security"
IMAGE_ID_RE = re.compile(r"sha256:[0-9a-f]{64}\Z")
TIMEOUT = 1800


@dataclass(frozen=True)
class Target:
    dockerfile: str
    stage: str | None = None


@dataclass
class BuiltImage:
    image_id: str | None
    archive: Path | None
    status: int


TARGETS = {
    "runtime": Target("backend/Dockerfile", "runtime"),
    "worker": Target("backend/Dockerfile", "worker"),
    "nginx": Target("frontend/Dockerfile", "web"),
    "postgres": Target("deployment/docker/postgres/Dockerfile"),
    "pgbouncer": Target("deployment/docker/pgbouncer/Dockerfile"),
}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("targets", nargs="*", choices=tuple(TARGETS))
    parser.add_argument("--reports", type=Path, required=True)
    parser.add_argument("--cache", type=Path, required=True)
    parser.add_argument("--docker", default="docker")
    return parser


@contextmanager
def target_lock(target: str) -> Iterator[None]:
    lock_dir = ROOT / "security" / ".cache"
    lock_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = lock_dir / f"scan-images-{target}.lock"
    fd = os.open(path, os.O_CREAT | os.O_RDWR | getattr(os, "O_NOFOLLOW", 0), 0o600)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise OSError("target lock is not a regular file")
        os.fchmod(fd, 0o600)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeError("target scan is already running") from error
        yield
    finally:
        os.close(fd)


def docker_run(
    docker: str,
    *args: str,
    capture: bool = False,
    capture_stderr: bool = False,
    timeout: int = TIMEOUT,
) -> subprocess.CompletedProcess:
    return subprocess.run(
        [docker, *args],
        cwd=ROOT,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture_stderr else None,
        timeout=timeout,
        check=False,
    )


def docker_is_rootless(docker: str) -> bool:
    result = docker_run(
        docker,
        "info",
        "--format",
        "{{json .SecurityOptions}}",
        capture=True,
        timeout=60,
    )
    if result.returncode:
        raise RuntimeError("cannot determine Docker daemon mode")
    try:
        options = json.loads(result.stdout)
    except (TypeError, json.JSONDecodeError) as error:
        raise RuntimeError("cannot parse Docker daemon security options") from error
    if not isinstance(options, list) or not all(
        isinstance(option, str) for option in options
    ):
        raise RuntimeError("Docker daemon security options have an unexpected shape")
    return any(
        option == "name=rootless" or option.startswith("name=rootless,")
        for option in options
    )


def prepare_report_dir(reports: Path, target: str) -> Path:
    images = reports / "images"
    images.mkdir(mode=0o700, exist_ok=True)
    if images.is_symlink() or not images.is_dir():
        raise OSError("image report path is not a directory")
    directory = images / target
    directory.mkdir(mode=0o700, exist_ok=True)
    if directory.is_symlink() or not directory.is_dir():
        raise OSError("image report path is not a directory")
    os.chmod(images, 0o700)
    os.chmod(directory, 0o700)
    for name in ("trivy-image.json", "table.txt", "image-id.txt"):
        path = directory / name
        try:
            mode = path.lstat().st_mode
        except FileNotFoundError:
            continue
        if stat.S_ISDIR(mode):
            raise OSError(f"image report path is a directory: {name}")
        path.unlink()
    return directory


def image_id_from(path: Path) -> str | None:
    try:
        value = path.read_text(encoding="ascii").strip()
    except (OSError, UnicodeError):
        return None
    return value if IMAGE_ID_RE.fullmatch(value) else None


@contextmanager
def build_image(
    target: str, image_name: str, docker: str, cache: Path, report_dir: Path
) -> Iterator[BuiltImage]:
    """Yield the exported image while its private temporary directory exists."""
    spec = TARGETS[target]
    with tempfile.TemporaryDirectory(prefix=f"scan-{target}-", dir=cache) as temporary:
        temporary_dir = Path(temporary)
        iidfile = temporary_dir / "image-id"
        archive = temporary_dir / "image.tar"
        artifact = BuiltImage(None, None, 0)
        try:
            args = [
                "build",
                "--pull",
                "--iidfile",
                str(iidfile),
                "--file",
                spec.dockerfile,
                "--tag",
                image_name,
            ]
            if spec.stage:
                args.extend(("--target", spec.stage))
            built = docker_run(docker, *args, ".")
            artifact.image_id = image_id_from(iidfile)
            if built.returncode:
                print(f"error: Docker build failed for {target}", file=sys.stderr)
                artifact.status = built.returncode or 1
            elif artifact.image_id is None:
                print(
                    f"error: Docker did not write a valid image ID for {target}",
                    file=sys.stderr,
                )
                artifact.status = 1
            else:
                inspected = docker_run(
                    docker,
                    "image",
                    "inspect",
                    "--format",
                    "{{.Id}}",
                    image_name,
                    capture=True,
                    timeout=60,
                )
                if (
                    inspected.returncode
                    or inspected.stdout.strip() != artifact.image_id
                ):
                    print(
                        f"error: built image tag does not match its ID for {target}",
                        file=sys.stderr,
                    )
                    artifact.status = 1
                else:
                    id_path = report_dir / "image-id.txt"
                    id_path.write_text(f"{artifact.image_id}\n", encoding="ascii")
                    os.chmod(id_path, 0o600)
                    saved = docker_run(
                        docker,
                        "image",
                        "save",
                        "--output",
                        str(archive),
                        artifact.image_id,
                    )
                    if (
                        saved.returncode
                        or not archive.is_file()
                        or archive.stat().st_size == 0
                    ):
                        print(
                            f"error: Docker could not export image {target}",
                            file=sys.stderr,
                        )
                        artifact.status = saved.returncode or 1
                    else:
                        os.chmod(archive, 0o600)
                        artifact.archive = archive
        except (OSError, subprocess.TimeoutExpired) as error:
            print(
                f"error: image build or export failed for {target}: {error.__class__.__name__}",
                file=sys.stderr,
            )
            artifact.status = 124 if isinstance(error, subprocess.TimeoutExpired) else 1

        try:
            yield artifact
        finally:
            if artifact.image_id is not None:
                try:
                    cleanup_status = cleanup_image(
                        target, image_name, artifact.image_id, docker
                    )
                    if cleanup_status and not artifact.status:
                        artifact.status = cleanup_status
                except (OSError, subprocess.TimeoutExpired):
                    print(
                        f"error: could not verify temporary image cleanup for {target}",
                        file=sys.stderr,
                    )
                    artifact.status = artifact.status or 1


def mount_arg(source: Path, target: str, *, readonly: bool = False) -> str:
    value = str(source.resolve())
    if "," in value:
        raise OSError("Docker mount paths cannot contain commas")
    option = ",readonly" if readonly else ""
    return f"type=bind,source={value},target={target}{option}"


def config_id_from_archive(path: Path) -> str | None:
    # Trivy reports the config digest; Docker's build IID may be a manifest digest.
    try:
        with tarfile.open(path, mode="r:*") as image:
            manifest_file = image.getmember("manifest.json")
            if not manifest_file.isfile() or manifest_file.size > 65536:
                return None
            manifest_stream = image.extractfile(manifest_file)
            if manifest_stream is None:
                return None
            manifest = json.loads(manifest_stream.read(65537))
            if not isinstance(manifest, list) or len(manifest) != 1:
                return None
            config_name = (
                manifest[0].get("Config") if isinstance(manifest[0], dict) else None
            )
            if not isinstance(config_name, str):
                return None
            config_path = PurePosixPath(config_name)
            if config_path.is_absolute() or ".." in config_path.parts:
                return None
            config_file = image.getmember(config_name)
            if not config_file.isfile() or config_file.size > 8 * 1024 * 1024:
                return None
            config_stream = image.extractfile(config_file)
            if config_stream is None:
                return None
            content = config_stream.read(8 * 1024 * 1024 + 1)
            if len(content) != config_file.size:
                return None
            return "sha256:" + hashlib.sha256(content).hexdigest()
    except (
        OSError,
        UnicodeError,
        tarfile.TarError,
        KeyError,
        ValueError,
        json.JSONDecodeError,
    ):
        return None


def run_trivy(
    docker: str,
    reports: Path,
    cache: Path,
    command: str,
    *args: str,
    archive: Path | None = None,
    rootless: bool = False,
) -> subprocess.CompletedProcess:
    mounts = [
        mount_arg(ROOT, "/src", readonly=True),
        mount_arg(reports, "/reports"),
        mount_arg(cache, "/cache"),
    ]
    if archive is not None:
        mounts.append(mount_arg(archive, "/image.tar", readonly=True))
    # Rootless Docker maps container root back to the invoking host user.
    container = [
        "run",
        "--rm",
        "--read-only",
        "--user",
        "0:0" if rootless else f"{os.getuid()}:{os.getgid()}",
        "--env",
        "HOME=/tmp",
        "--env",
        "TMPDIR=/tmp",
        "--tmpfs",
        "/tmp:rw,noexec,nosuid,size=1g",
    ]
    for mount in mounts:
        container.extend(("--mount", mount))
    container.extend(("--workdir", "/src", SCANNER_IMAGE, command, *args))
    return docker_run(docker, *container)


def report_is_valid(path: Path, expected_id: str) -> bool:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        return False
    if (
        not isinstance(document, dict)
        or document.get("ArtifactType") != "container_image"
    ):
        return False
    results = document.get("Results")
    if (
        not isinstance(results, list)
        or not results
        or not all(isinstance(item, dict) for item in results)
    ):
        return False
    metadata = document.get("Metadata")
    return isinstance(metadata, dict) and metadata.get("ImageID") == expected_id


def scan_archive(
    target: str,
    image_id: str,
    archive: Path,
    reports: Path,
    cache: Path,
    docker: str,
    rootless: bool,
) -> int:
    report_dir = reports / "images" / target
    report_path = report_dir / "trivy-image.json"
    config_id = config_id_from_archive(archive)
    if config_id is None:
        print(
            f"error: Docker archive has no valid image config for {target}",
            file=sys.stderr,
        )
        return 1
    scanned = run_trivy(
        docker,
        reports,
        cache,
        "image",
        "--no-progress",
        "--config",
        "/src/security/trivy/image.yml",
        "--cache-dir",
        "/cache/trivy",
        "--input",
        "/image.tar",
        "--format",
        "json",
        "--output",
        f"/reports/images/{target}/trivy-image.json",
        archive=archive,
        rootless=rootless,
    )
    if report_path.exists():
        os.chmod(report_path, 0o600)
    if not report_is_valid(report_path, config_id):
        print(
            f"error: Trivy did not create a valid image report for {target}",
            file=sys.stderr,
        )
        return scanned.returncode or 1

    table_path = report_dir / "table.txt"
    converted = run_trivy(
        docker,
        reports,
        cache,
        "convert",
        "--format",
        "table",
        "--table-mode",
        "summary",
        "--scanners",
        "vuln,misconfig",
        "--output",
        f"/reports/images/{target}/table.txt",
        f"/reports/images/{target}/trivy-image.json",
        rootless=rootless,
    )
    if table_path.exists():
        os.chmod(table_path, 0o600)
        print(table_path.read_text(encoding="utf-8"), end="")
    if scanned.returncode:
        return scanned.returncode
    if converted.returncode or not table_path.is_file():
        print(f"error: Trivy table conversion failed for {target}", file=sys.stderr)
        return converted.returncode or 1
    return 0


def cleanup_image(target: str, image_name: str, image_id: str, docker: str) -> int:
    current = docker_run(
        docker,
        "image",
        "inspect",
        "--format",
        "{{.Id}}",
        image_name,
        capture=True,
        capture_stderr=True,
        timeout=60,
    )
    if current.returncode:
        listed = docker_run(
            docker,
            "image",
            "ls",
            "--quiet",
            "--no-trunc",
            "--filter",
            f"reference={image_name}",
            capture=True,
            timeout=60,
        )
        if listed.returncode:
            print(
                f"error: cannot verify temporary image cleanup for {target}",
                file=sys.stderr,
            )
            return 1
        if listed.stdout.strip():
            print(
                f"error: cannot inspect temporary image tag for {target}",
                file=sys.stderr,
            )
            return 1
        return 0
    if current.stdout.strip() != image_id:
        print(
            f"error: temporary image tag changed; leaving it untouched for {target}",
            file=sys.stderr,
        )
        return 1
    removed = docker_run(docker, "image", "rm", "--no-prune", image_name, timeout=60)
    if removed.returncode:
        print(
            f"error: could not remove temporary image tag for {target}", file=sys.stderr
        )
        return 1
    return 0


def scan_target_locked(
    target: str, reports: Path, cache: Path, docker: str, rootless: bool
) -> int:
    image_name = f"{IMAGE_PREFIX}:{target}"
    status = 0
    try:
        report_dir = prepare_report_dir(reports, target)
        (cache / "trivy").mkdir(mode=0o700, parents=True, exist_ok=True)
        existing = docker_run(
            docker,
            "image",
            "ls",
            "--quiet",
            "--no-trunc",
            "--filter",
            f"reference={image_name}",
            capture=True,
            timeout=60,
        )
        if existing.returncode:
            print(
                f"error: cannot check temporary image tag for {target}", file=sys.stderr
            )
            status = 1
        elif existing.stdout.strip():
            print(
                f"error: temporary image tag already exists for {target}",
                file=sys.stderr,
            )
            status = 1
        else:
            with build_image(target, image_name, docker, cache, report_dir) as built:
                status = built.status
                if (
                    status == 0
                    and built.archive is not None
                    and built.image_id is not None
                ):
                    status = scan_archive(
                        target,
                        built.image_id,
                        built.archive,
                        reports,
                        cache,
                        docker,
                        rootless,
                    )
            status = status or built.status
    except (OSError, subprocess.TimeoutExpired) as error:
        print(
            f"error: image scan could not complete for {target}: {error.__class__.__name__}",
            file=sys.stderr,
        )
        status = 124 if isinstance(error, subprocess.TimeoutExpired) else 1
    return status


def scan_target(
    target: str, reports: Path, cache: Path, docker: str, rootless: bool = False
) -> int:
    try:
        with target_lock(target):
            return scan_target_locked(target, reports, cache, docker, rootless)
    except (OSError, RuntimeError) as error:
        print(f"error: {error} for {target}", file=sys.stderr)
        return 1


def run_targets(
    targets: Sequence[str],
    reports: Path,
    cache: Path,
    docker: str,
    rootless: bool = False,
) -> int:
    status = 0
    for target in targets or tuple(TARGETS):
        result = scan_target(target, reports, cache, docker, rootless)
        if result and not status:
            status = result
    return status


def main(argv: Sequence[str] | None = None) -> int:
    old_umask = os.umask(0o077)
    try:
        args = build_parser().parse_args(argv)
        reports = args.reports.absolute()
        cache = args.cache.absolute()
        reports.mkdir(mode=0o700, parents=True, exist_ok=True)
        cache.mkdir(mode=0o700, parents=True, exist_ok=True)
        executable = shutil.which(args.docker)
        if not executable:
            print("error: Docker executable was not found", file=sys.stderr)
            return 2
        rootless = docker_is_rootless(executable)
        return run_targets(args.targets, reports, cache, executable, rootless)
    except (OSError, RuntimeError, subprocess.TimeoutExpired) as error:
        print(
            f"error: image scan setup failed: {error.__class__.__name__}",
            file=sys.stderr,
        )
        return 2
    finally:
        os.umask(old_umask)


if __name__ == "__main__":
    raise SystemExit(main())

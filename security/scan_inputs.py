#!/usr/bin/env python3
"""Run Gitleaks against current Git inputs and the complete repository history."""

import argparse
import errno
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile

TIMEOUT = 300
REPORTS = ("gitleaks-files.json", "gitleaks-history.json", "gitleaks-version.txt")


def _git(root, *args):
    return subprocess.run(
        ["git", "-C", str(root), *args], stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL, timeout=TIMEOUT, check=False,
    )


def _checkout_ok(root):
    top = _git(root, "rev-parse", "--show-toplevel")
    shallow = _git(root, "rev-parse", "--is-shallow-repository")
    if top.returncode or Path(os.fsdecode(top.stdout).strip()).resolve() != root:
        print("error: --root must be the Git checkout root", file=sys.stderr)
        return False
    if shallow.returncode or shallow.stdout.strip() != b"false":
        print(
            "error: shallow Git checkout cannot provide complete history",
            file=sys.stderr,
        )
        return False
    return True


def _open_input(root_fd, parts):
    flags = os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0)
    parent_fd = os.dup(root_fd)
    try:
        for part in parts[:-1]:
            next_fd = os.open(part, flags, dir_fd=parent_fd)
            os.close(parent_fd)
            parent_fd = next_fd
        source_flags = (
            os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
        )
        source_fd = os.open(parts[-1], source_flags, dir_fd=parent_fd)
    except OSError as error:
        if error.errno in (errno.ENOENT, errno.ENOTDIR, errno.ELOOP):
            return None
        raise
    finally:
        os.close(parent_fd)
    try:
        regular = stat.S_ISREG(os.fstat(source_fd).st_mode)
    except OSError:
        os.close(source_fd)
        raise
    if regular:
        return source_fd
    os.close(source_fd)
    return None


def _copy_inputs(root, destination):
    listing = _git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
    if listing.returncode:
        raise OSError("git ls-files failed")
    root_fd = os.open(root, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        for raw in listing.stdout.split(b"\0"):
            if not raw:
                continue
            parts = os.fsdecode(raw).split("/")
            if any(part in ("", ".", "..") for part in parts):
                continue
            source_fd = _open_input(root_fd, parts)
            if source_fd is None:
                continue
            target = destination.joinpath(*parts)
            with os.fdopen(source_fd, "rb") as source:
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with target.open("xb") as output:
                    shutil.copyfileobj(source, output)
    finally:
        os.close(root_fd)


def _run(scanner, args, *, cwd, stdout=subprocess.DEVNULL):
    try:
        return subprocess.run(
            [scanner, *args], cwd=cwd, stdout=stdout, stderr=subprocess.DEVNULL,
            timeout=TIMEOUT, check=False,
        ).returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def _scan(scanner, command, config, report, path, *, cwd, options=()):
    return _run(scanner, [
        command, "--config", str(config), "--redact=100",
        "--ignore-gitleaks-allow", "--no-banner", "--no-color", *options,
        "--report-format=json", f"--report-path={report}", str(path),
    ], cwd=cwd)


def _valid_report(path):
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        valid = isinstance(data, list) and all(isinstance(item, dict) for item in data)
        return valid, bool(data)
    except (OSError, UnicodeError, json.JSONDecodeError):
        return False, False


def run_scan(root, scanner, config, reports, cache):
    root = Path(root).resolve(strict=True)
    config = Path(config).resolve()
    reports = Path(reports).absolute()
    cache = Path(cache).absolute()
    reports.mkdir(mode=0o700, parents=True, exist_ok=True)
    cache.mkdir(mode=0o700, parents=True, exist_ok=True)
    for name in REPORTS:
        (reports / name).unlink(missing_ok=True)
    executable = shutil.which(str(scanner))
    if not executable:
        print("error: Gitleaks executable was not found", file=sys.stderr)
        return 2
    if not config.is_file() or not _checkout_ok(root):
        if not config.is_file():
            print("error: Gitleaks config file was not found", file=sys.stderr)
        return 2

    status = 0
    version_path = reports / REPORTS[2]
    try:
        with version_path.open("wb") as version:
            if not _run(executable, ["version"], cwd=root, stdout=version):
                status = 1
    except OSError:
        print("error: version report could not be written", file=sys.stderr)
        status = 1

    files_path, history_path = (reports / name for name in REPORTS[:2])
    try:
        with tempfile.TemporaryDirectory(prefix="gitleaks-", dir=cache) as temporary:
            scan_root = Path(temporary)
            os.chmod(scan_root, 0o700)
            _copy_inputs(root, scan_root)
            current_ok = _scan(
                executable, "dir", config, files_path, ".", cwd=scan_root,
            )
    except (OSError, subprocess.TimeoutExpired):
        print("error: current-input scan could not be prepared", file=sys.stderr)
        current_ok = False
    history_ok = _scan(
        executable, "git", config, history_path, root, cwd=root,
        options=("--log-opts=--all",),
    )
    if not current_ok or not history_ok:
        status = 1

    for path in (files_path, history_path):
        valid, findings = _valid_report(path)
        if not valid:
            print(
                f"error: missing or invalid JSON list report: {path.name}",
                file=sys.stderr,
            )
            status = 1
        elif findings:
            status = 1
    return status


def main():
    old_umask = os.umask(0o077)
    try:
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument("--root", type=Path, required=True)
        parser.add_argument("--scanner", required=True)
        parser.add_argument("--config", type=Path, required=True)
        parser.add_argument("--reports", type=Path, required=True)
        parser.add_argument("--cache", type=Path, required=True)
        args = parser.parse_args()
        try:
            return run_scan(
                args.root, args.scanner, args.config, args.reports, args.cache,
            )
        except (OSError, subprocess.TimeoutExpired):
            print("error: Gitleaks check failed", file=sys.stderr)
            return 1
    finally:
        os.umask(old_umask)


if __name__ == "__main__":
    raise SystemExit(main())

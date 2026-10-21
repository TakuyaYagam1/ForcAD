"""Remove completed rollback transactions after runtime restoration succeeds."""

import hashlib
import json
import os
import re
from pathlib import Path

from .common import regular
from .files import atomic_write


def prepare_history(bundle):
    base = bundle.path(f"/var/backups/forcad/{bundle.role}")
    regular(base / "check")
    marker = regular(base / "directories-owned.json")
    if not marker.exists():
        legacy = bundle.manifest.exists() or regular(base / "live.json").exists()
        directories = [
            str(path.relative_to(bundle.root))
            for path in (base.parent, base)
            if not path.exists() or legacy
        ]
        base.mkdir(parents=True, exist_ok=True, mode=0o700)
        atomic_write(
            marker,
            (json.dumps(directories) + "\n").encode(),
            0o600,
            os.geteuid(),
            os.getegid(),
        )
    return base


def transaction_files(directory):
    """Recognize old and new backup layouts without claiming extra files."""
    if directory.is_symlink() or not directory.is_dir():
        return None
    if not re.fullmatch(
        r"(install|uninstall|apply)-\d{8}-\d{6}-[\w-]+", directory.name
    ):
        return None
    try:
        records = json.loads(regular(directory / "files.json").read_text())
        json.loads(regular(directory / "directories.json").read_text())
        expected = {"files.json", "directories.json"}
        for record in records:
            name = Path(record["path"])
            if not name.is_absolute() or ".." in name.parts:
                return None
            if record["existed"]:
                relative = str(name).lstrip("/")
                data = regular(directory / relative).read_bytes()
                if hashlib.sha256(data).hexdigest() != record["sha256"]:
                    return None
                expected.add(relative)
        for name in ("live.json", "live-before.json"):
            path = regular(directory / name)
            if path.exists():
                json.loads(path.read_text())
                expected.add(name)
        entries = list(directory.rglob("*"))
        if any(path.is_symlink() for path in entries):
            return None
        actual = {
            str(path.relative_to(directory)) for path in entries if path.is_file()
        }
        if actual != expected:
            return None
        parents = {parent for name in expected for parent in Path(name).parents}
        if any(
            path.is_dir() and path.relative_to(directory) not in parents
            for path in entries
        ):
            return None
        return entries
    except (OSError, ValueError, KeyError, TypeError):
        return None


def cleanup_history(bundle):
    base = bundle.path(f"/var/backups/forcad/{bundle.role}")
    regular(base / "check")
    if not base.exists():
        return
    if bundle.manifest.exists() or regular(base / "live.json").exists():
        raise ValueError("Cannot remove rollback history before uninstall completes")
    marker = regular(base / "directories-owned.json")
    owned = json.loads(marker.read_text()) if marker.exists() else []
    allowed = {str(path.relative_to(bundle.root)) for path in (base, base.parent)}
    if not isinstance(owned, list) or any(name not in allowed for name in owned):
        raise ValueError("Invalid backup directory ownership record")
    for directory in list(base.iterdir()):
        entries = transaction_files(directory)
        if entries is None:
            continue
        for path in sorted(entries, key=lambda path: len(path.parts), reverse=True):
            if path.is_dir():
                path.rmdir()
            else:
                path.unlink()
        directory.rmdir()
    regular(base / "operation.lock").unlink(missing_ok=True)
    marker.unlink(missing_ok=True)
    for name in sorted(owned, key=lambda name: len(Path(name).parts), reverse=True):
        path = bundle.path("/" + name)
        if path.exists() and not any(path.iterdir()):
            path.rmdir()

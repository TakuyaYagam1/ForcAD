"""Transactional host file installation and rollback."""

import hashlib
import json
import os
import shutil
import stat
import tempfile
import time
from pathlib import Path

from .common import regular


def atomic_write(path, data, mode, uid, gid):
    regular(path)
    missing = []
    parent = path.parent
    while not parent.exists():
        missing.append(parent)
        parent = parent.parent
    for parent in reversed(missing):
        parent.mkdir(mode=0o700 if parent == Path("/etc/wireguard") else 0o755)
        parent.chmod(0o700 if parent == Path("/etc/wireguard") else 0o755)
        os.chown(parent, uid, gid)
    fd, name = tempfile.mkstemp(prefix=".install-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            os.fchmod(stream.fileno(), mode)
            os.fchown(stream.fileno(), uid, gid)
        os.replace(name, path)
    finally:
        Path(name).unlink(missing_ok=True)


def file_state(path):
    regular(path)
    if not path.exists():
        return {"existed": False}
    info = path.stat()
    return {
        "existed": True,
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        "mode": stat.S_IMODE(info.st_mode),
        "uid": info.st_uid,
        "gid": info.st_gid,
    }


def restore_file(path, record, data):
    if record["existed"]:
        atomic_write(path, data, record["mode"], record["uid"], record["gid"])
    else:
        regular(path).unlink(missing_ok=True)


def restore_directories(root, records):
    failures = []
    for item in sorted(
        records, key=lambda item: len(Path(item["path"]).parts), reverse=True
    ):
        path = root / item["path"].lstrip("/")
        try:
            regular(path / "check")
            if not path.exists():
                continue
            if item["existed"]:
                path.chmod(item["mode"])
                os.chown(path, item["uid"], item["gid"])
            elif not any(path.iterdir()):
                path.rmdir()
        except (OSError, ValueError) as error:
            failures.append(f"{path}: {error}")
    if failures:
        raise OSError("Could not restore directories: " + "; ".join(failures))


def rollback_files(root, backup, saved, written, directories):
    failures = []
    for path in reversed(written):
        item = saved[path]
        try:
            current = file_state(path)
            if all(
                current.get(key) == item.get(key)
                for key in ("existed", "sha256", "mode", "uid", "gid")
            ):
                continue
            data = (
                (backup / path.relative_to(root)).read_bytes()
                if item["existed"]
                else None
            )
            restore_file(path, item, data)
        except (OSError, ValueError) as error:
            failures.append(f"{path}: {error}")
    try:
        restore_directories(root, directories)
    except (OSError, ValueError) as error:
        failures.append(str(error))
    if failures:
        raise OSError(
            f"Rollback incomplete; recover from {backup}: " + "; ".join(failures)
        )


class FileOperations:
    def uninstall_owned_paths(self):
        """Role-specific files to remove even when installation adopted them."""
        return set()

    def install_files(self):
        from .access import operator

        previous_manifest = (
            json.loads(regular(self.manifest).read_text())
            if self.manifest.exists()
            else {}
        )
        if previous_manifest and previous_manifest.get("format") != 2:
            raise ValueError(
                "Legacy installation has no verified rollback history. Restore its original backup manually before reinstalling."
            )
        self.owner = operator(previous_manifest.get("owner"))
        records = []
        for path, (data, mode) in self.changes.items():
            if path == self.manifest:
                continue
            regular(path)
            owner = path if path.exists() else path.parent
            while not owner.exists():
                owner = owner.parent
            records.append(
                {
                    "path": "/" + str(path.relative_to(self.root)),
                    "sha256": hashlib.sha256(data).hexdigest(),
                    "mode": mode,
                    "uid": self.owner["uid"] if self.owner else owner.stat().st_uid,
                    "gid": self.owner["gid"] if self.owner else owner.stat().st_gid,
                }
            )
        manifest = {
            "format": 2,
            "config": self.config,
            "options": self.options,
            "files": records,
            "rollback": previous_manifest.get("rollback"),
        }
        if self.owner:
            manifest["owner"] = self.owner
        self.put(
            f"/etc/forcad/{self.role}/install.json",
            (json.dumps(manifest, indent=2) + "\n").encode(),
        )
        changed = [
            (path, data, mode)
            for path, (data, mode) in self.changes.items()
            if not path.exists()
            or path.read_bytes() != data
            or stat.S_IMODE(path.stat().st_mode) != mode
            or (
                self.owner
                and (path.stat().st_uid, path.stat().st_gid)
                != (self.owner["uid"], self.owner["gid"])
            )
        ]
        directories = self.directory_changes(list(self.changes))
        if not changed and not directories:
            print("Already installed; no files changed.")
            return
        # Every successful transaction links its predecessor. Failed attempts
        # leave backups for inspection but never enter the uninstall history.
        paths = [path for path, _, _ in changed if path != self.manifest]
        paths.append(self.manifest)
        # Directory-only changes still need their children in rollback metadata.
        for item in directories:
            for path in self.changes:
                if self.path(item["path"]) in path.parents and path not in paths:
                    paths.append(path)
        backup, saved = self.backup(paths, "install", directories)
        manifest["rollback"] = {
            "backup": "/" + str(backup.relative_to(self.root)),
            "files_sha256": hashlib.sha256(
                (backup / "files.json").read_bytes()
            ).hexdigest(),
            "directories_sha256": hashlib.sha256(
                (backup / "directories.json").read_bytes()
            ).hexdigest(),
        }
        self.changes[self.manifest] = (
            (json.dumps(manifest, indent=2) + "\n").encode(),
            0o600,
        )
        # The unprivileged Zeek service needs to traverse these directories.
        # Secret files retain mode 0600; only its public scripts are readable.
        written = []
        try:
            for item in sorted(
                directories, key=lambda item: len(Path(item["path"]).parts)
            ):
                path = self.path(item["path"])
                if not item["existed"]:
                    path.mkdir(mode=item["installed_mode"])
                path.chmod(item["installed_mode"])
                os.chown(path, item["installed_uid"], item["installed_gid"])
            for directory in ("/etc/forcad", "/etc/forcad/monitoring"):
                if directory.endswith("monitoring") and not (
                    self.role == "router"
                    and (
                        self.options["network_metrics"] or self.options["game_metrics"]
                    )
                ):
                    continue
                path = self.path(directory)
                regular(path / "check")
                path.mkdir(parents=True, exist_ok=True)
                path.chmod(0o755)
            for path in paths:
                data, mode = self.changes[path]
                record = next(
                    (item for item in records if self.path(item["path"]) == path), None
                )
                uid, gid = (
                    (record["uid"], record["gid"])
                    if record
                    else (
                        (self.owner["uid"], self.owner["gid"])
                        if self.owner
                        else (os.geteuid(), os.getegid())
                    )
                )
                written.append(path)
                atomic_write(path, data, mode, uid, gid)
        except BaseException:
            rollback_files(self.root, backup, saved, written, directories)
            raise
        print(f"Installed {len(paths)} files. Backup: {backup}")

    def directory_changes(self, paths):
        records = {}
        access_owner = getattr(self, "owner", None)
        managed = {"/etc/forcad", f"/etc/forcad/{self.role}"}
        if self.role == "vpn":
            managed.add("/etc/wireguard")
        if self.role == "router" and (
            self.options["network_metrics"] or self.options["game_metrics"]
        ):
            managed.add("/etc/forcad/monitoring")
        for path in paths:
            parent = "/" + str(path.parent.relative_to(self.root))
            if parent.startswith("/etc/systemd/system/") and parent.endswith(".d"):
                managed.add(parent)
        for path in paths:
            for parent in path.parents:
                if parent.exists():
                    break
                name = "/" + str(parent.relative_to(self.root))
                owner = parent.parent
                while not owner.exists():
                    owner = owner.parent
                records[name] = {
                    "path": name,
                    "existed": False,
                    "installed_mode": 0o700 if name == "/etc/wireguard" else 0o755,
                    "installed_uid": owner.stat().st_uid,
                    "installed_gid": owner.stat().st_gid,
                }
                if access_owner and name in managed:
                    records[name].update(
                        installed_uid=access_owner["uid"],
                        installed_gid=access_owner["gid"],
                    )
        for name in ("/etc/forcad", "/etc/forcad/monitoring"):
            if name.endswith("monitoring") and not (
                self.role == "router"
                and (self.options["network_metrics"] or self.options["game_metrics"])
            ):
                continue
            path = self.path(name)
            regular(path / "check")
            if path.exists() and stat.S_IMODE(path.stat().st_mode) != 0o755:
                info = path.stat()
                records[name] = {
                    "path": name,
                    "existed": True,
                    "mode": stat.S_IMODE(info.st_mode),
                    "uid": info.st_uid,
                    "gid": info.st_gid,
                    "installed_mode": 0o755,
                    "installed_uid": info.st_uid,
                    "installed_gid": info.st_gid,
                }
        if access_owner:
            for name in managed:
                path = self.path(name)
                regular(path / "check")
                if name in records:
                    records[name].update(
                        installed_uid=access_owner["uid"],
                        installed_gid=access_owner["gid"],
                    )
                elif path.exists():
                    info = path.stat()
                    mode = 0o700 if name == "/etc/wireguard" else 0o755
                    if (stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) != (
                        mode,
                        access_owner["uid"],
                        access_owner["gid"],
                    ):
                        records[name] = {
                            "path": name,
                            "existed": True,
                            "mode": stat.S_IMODE(info.st_mode),
                            "uid": info.st_uid,
                            "gid": info.st_gid,
                            "installed_mode": mode,
                            "installed_uid": access_owner["uid"],
                            "installed_gid": access_owner["gid"],
                        }
        return list(records.values())

    def backup(self, paths, action, directories=()):
        from .history import prepare_history

        base = prepare_history(self)
        backup = Path(
            tempfile.mkdtemp(prefix=time.strftime(f"{action}-%Y%m%d-%H%M%S-"), dir=base)
        )
        saved = {}
        for path in paths:
            item = file_state(path)
            item["path"] = "/" + str(path.relative_to(self.root))
            if item["existed"]:
                target = backup / path.relative_to(self.root)
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                shutil.copy2(path, target)
                target.chmod(0o600)
            saved[path] = item
        for name, value in (
            ("files.json", list(saved.values())),
            ("directories.json", list(directories)),
        ):
            path = backup / name
            path.write_text(json.dumps(value, indent=2) + "\n")
            path.chmod(0o600)
        return backup, saved

    def uninstall_history(self, installed):
        originals, directories, expected = {}, {}, {}
        visited = set()
        manifest_name = "/" + str(self.manifest.relative_to(self.root))
        while True:
            if installed.get("format") != 2 or not installed.get("rollback"):
                raise ValueError(
                    "Legacy installation has no verified rollback history. Restore its original backup manually; no files changed."
                )
            if installed["config"]["role"] != self.role:
                raise ValueError("Rollback history belongs to another host role")
            backup = self.path(installed["rollback"]["backup"])
            if (
                backup.parent != self.path(f"/var/backups/forcad/{self.role}")
                or backup in visited
            ):
                raise ValueError("Invalid rollback backup path or cycle")
            visited.add(backup)
            for item in installed["files"]:
                self.path(item["path"])
                expected.setdefault(item["path"], item)
            allowed = {item["path"] for item in installed["files"]} | {manifest_name}
            for name in ("files", "directories"):
                data = regular(backup / f"{name}.json").read_bytes()
                if (
                    hashlib.sha256(data).hexdigest()
                    != installed["rollback"][f"{name}_sha256"]
                ):
                    raise ValueError(
                        f"Corrupt rollback backup metadata: {backup / (name + '.json')}"
                    )
            saved = json.loads(regular(backup / "files.json").read_text())
            names = set()
            previous = None
            for item in saved:
                name = item["path"]
                if name not in allowed or name in names:
                    raise ValueError("Unexpected file in rollback backup")
                names.add(name)
                self.path(name)
                data = None
                if item["existed"]:
                    data = regular(backup / name.lstrip("/")).read_bytes()
                    if hashlib.sha256(data).hexdigest() != item["sha256"]:
                        raise ValueError(f"Corrupt rollback backup: {name}")
                    for field in ("mode", "uid", "gid"):
                        if not isinstance(item[field], int) or item[field] < 0:
                            raise ValueError("Invalid rollback file metadata")
                originals[name] = (item, data)
                if name == manifest_name:
                    previous = json.loads(data) if item["existed"] else {}
            if previous is None:
                raise ValueError("Rollback backup is missing the installation manifest")
            parents = {
                str(parent)
                for name in names
                for parent in Path(name).parents
                if str(parent) != "/"
            }
            for item in json.loads(regular(backup / "directories.json").read_text()):
                if item["path"] not in parents:
                    raise ValueError("Unexpected directory in rollback backup")
                latest = directories.get(item["path"], item)
                directories[item["path"]] = {
                    **item,
                    **{
                        key: latest[key]
                        for key in ("installed_mode", "installed_uid", "installed_gid")
                    },
                }
            if not previous:
                return originals, directories, expected
            installed = previous

    def _uninstall_plan(self, force=False):
        regular(self.manifest)
        if not self.manifest.exists():
            return {
                "active": False,
                "changes": [],
                "directories": {},
                "directory_before": [],
                "conflicts": [],
            }
        self.check_role()
        installed = json.loads(self.manifest.read_text())
        self.options = installed["options"]
        self.config = installed["config"]
        originals, directories, expected = self.uninstall_history(installed)
        owned = self.uninstall_owned_paths() & expected.keys()
        for name in owned:
            # Identical adopted files appear in the manifest but not in backups.
            originals.setdefault(name, ({"existed": True, **expected[name]}, None))
        changes, conflicts = [], []
        manifest_name = "/" + str(self.manifest.relative_to(self.root))
        for name, (original, data) in originals.items():
            path = self.path(name)
            current = file_state(path)
            fields = ("existed", "sha256", "mode", "uid", "gid")
            target = {"existed": False} if name in owned else original
            if all(current.get(key) == target.get(key) for key in fields):
                continue
            if name != manifest_name:
                wanted = {"existed": True, **expected[name]}
                active = (
                    name == "/etc/forcad/router/active.nft"
                    and current["existed"]
                    and path.read_bytes().strip()
                    in (
                        b'include "/etc/forcad/router/game.nft"',
                        b'include "/etc/forcad/router/pre-game.nft"',
                    )
                )
                if (
                    original["existed"]
                    and not all(current.get(key) == original.get(key) for key in fields)
                    and any(
                        current.get(key) != wanted.get(key)
                        for key in fields
                        if not (active and key == "sha256")
                    )
                ):
                    conflicts.append(name)
            changes.append((path, target, None if name in owned else data))
        directory_before = []
        for name, item in directories.items():
            path = self.path(name)
            regular(path / "check")
            info = path.stat() if path.exists() else None
            if info and not stat.S_ISDIR(info.st_mode):
                raise ValueError(f"Not a directory: {path}")
            state = {"path": name, "existed": info is not None}
            if info:
                state.update(
                    mode=stat.S_IMODE(info.st_mode), uid=info.st_uid, gid=info.st_gid
                )
                fields = ("mode", "uid", "gid")
                if any(
                    state[key] != item["installed_" + key] for key in fields
                ) and not (
                    item["existed"] and all(state[key] == item[key] for key in fields)
                ):
                    conflicts.append(name)
            directory_before.append(state)
        if conflicts and not force:
            raise ValueError(
                "Files changed since installation: "
                + ", ".join(conflicts)
                + ". Review them, then use --force to back them up and restore the original installation state."
            )
        return {
            "active": True,
            "changes": changes,
            "directories": directories,
            "directory_before": directory_before,
            "conflicts": conflicts,
        }

    def preflight_uninstall(self, force=False):
        """Return a validated, compact uninstall plan without changing files."""
        plan = self._uninstall_plan(force=force)
        actions = []
        for path, original, _ in plan["changes"]:
            actions.append(
                {
                    "path": "/" + str(path.relative_to(self.root)),
                    "action": "restore" if original["existed"] else "remove",
                    "original": {
                        key: original.get(key)
                        for key in ("existed", "sha256", "mode", "uid", "gid")
                    },
                    "current": file_state(path),
                }
            )
        return {
            "role": self.role,
            "manifest": "/" + str(self.manifest.relative_to(self.root)),
            "installed": plan["active"],
            "actions": actions,
            "directories": plan["directory_before"],
            "conflicts": list(plan["conflicts"]),
            "force": force,
        }

    def uninstall_files(self, force=False, dry_run=False):
        plan = self._uninstall_plan(force=force)
        if not plan["active"]:
            print("Nothing installed for this host role; no files changed.")
            return
        changes = plan["changes"]
        directories = plan["directories"]
        directory_before = plan["directory_before"]
        for path, original, _ in changes:
            print(f"{'Restore' if original['existed'] else 'Remove'} {path}")
        if dry_run:
            print(
                "Dry run; no files changed. Live settings and services are not reverted."
            )
            return self.preflight_uninstall(force=force)
        changes.sort(key=lambda change: change[0] == self.manifest)
        backup, saved = self.backup(
            [path for path, _, _ in changes], "uninstall", directory_before
        )
        print(f"Current files saved: {backup}")
        written = []
        try:
            for path, original, data in changes:
                if path == self.manifest:
                    continue
                written.append(path)
                restore_file(path, original, data)
            restore_directories(self.root, list(directories.values()))
            for path, original, data in changes:
                if path == self.manifest:
                    written.append(path)
                    restore_file(path, original, data)
            restore_directories(self.root, list(directories.values()))
        except BaseException:
            rollback_files(self.root, backup, saved, written, directory_before)
            raise
        print(
            "Uninstalled host files; shared files restored and managed files removed."
        )

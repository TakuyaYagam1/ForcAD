"""Coordinate file transactions with reversible router runtime changes."""

import fcntl
import hashlib
import json
import os
from contextlib import contextmanager
from copy import deepcopy

from hostlib.common import regular
from hostlib.files import atomic_write, rollback_files

from .network import Network
from .system import System


class Lifecycle:
    def __init__(self, bundle):
        self.bundle = bundle
        self.network = Network(bundle)
        self.system = System(bundle)
        self.journal = bundle.path("/var/backups/forcad/router/live.json")

    @contextmanager
    def locked(self):
        regular(self.journal)
        self.journal.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        lock = regular(self.journal.parent / "operation.lock")
        with lock.open("a") as stream:
            os.fchmod(stream.fileno(), 0o600)
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            yield

    def load(self):
        if not regular(self.journal).exists():
            if self.bundle.manifest.exists():
                installed = json.loads(regular(self.bundle.manifest).read_text())
                if installed["options"].get("router_live"):
                    raise ValueError(
                        "Live recovery journal is missing; restore live.json from backup before continuing"
                    )
            return None
        envelope = json.loads(self.journal.read_text())
        record = envelope["record"]
        encoded = json.dumps(record, sort_keys=True).encode()
        if (
            envelope.get("sha256") != hashlib.sha256(encoded).hexdigest()
            or record.get("format") != 1
            or not isinstance(record.get("baseline"), dict)
            or record.get("phase") != "active"
        ):
            raise ValueError(
                f"Incomplete or corrupt router recovery journal: {self.journal}. "
                "Inspect its saved state before continuing."
            )
        return record

    def save(self, record):
        envelope = {
            "record": record,
            "sha256": hashlib.sha256(
                json.dumps(record, sort_keys=True).encode()
            ).hexdigest(),
        }
        atomic_write(
            self.journal,
            (json.dumps(envelope, indent=2) + "\n").encode(),
            0o600,
            os.geteuid(),
            os.getegid(),
        )

    def snapshot(self, options, network_config=None):
        return {
            "network": self.network.snapshot(config=network_config),
            "system": self.system.snapshot(options),
        }

    def checkpoint(self, paths, directories):
        backup, saved = self.bundle.backup(paths, "apply", directories)
        return backup, saved, list(paths), directories

    @staticmethod
    def save_runtime(checkpoint, state):
        atomic_write(
            checkpoint[0] / "live-before.json",
            (json.dumps(state, indent=2) + "\n").encode(),
            0o600,
            os.geteuid(),
            os.getegid(),
        )

    def archive_journal(self, checkpoint):
        if self.journal.exists():
            atomic_write(
                checkpoint[0] / "live.json",
                self.journal.read_bytes(),
                0o600,
                os.geteuid(),
                os.getegid(),
            )

    def game_mode_changed(self):
        installed = json.loads(regular(self.bundle.manifest).read_text())
        name = "/etc/forcad/router/active.nft"
        data = regular(self.bundle.path(name)).read_bytes()
        record = next(item for item in installed["files"] if item["path"] == name)
        return (
            data.strip()
            in (
                b'include "/etc/forcad/router/game.nft"',
                b'include "/etc/forcad/router/pre-game.nft"',
            )
            and hashlib.sha256(data).hexdigest() != record["sha256"]
        )

    def check_drift(self, previous, before, force=False):
        if "applied" not in previous:
            raise ValueError(
                "Router journal has no applied snapshot; recover its original backup first"
            )
        self.network.check_restore(
            previous["baseline"]["network"],
            previous["applied"]["network"],
            before["network"],
            force=force,
        )
        self.system.check_restore(
            previous["baseline"]["system"],
            previous["applied"]["system"],
            before["system"],
            force=force,
            allow_game_switch=self.game_mode_changed(),
        )

    def restore_files(self, checkpoint):
        backup, saved, paths, directories = checkpoint
        rollback_files(self.bundle.root, backup, saved, paths, directories)

    def recover(
        self, checkpoint, before, options, previous, error, allowed_current=None
    ):
        failures = []
        for action in (
            lambda: self.system.stop(options),
            lambda: self.restore_files(checkpoint),
            lambda: self.network.restore(
                before["network"],
                **({"allowed_current": allowed_current} if allowed_current else {}),
            ),
            lambda: self.system.restore(before["system"]),
        ):
            try:
                action()
            except Exception as failure:  # noqa: BLE001 - attempt every rollback action
                failures.append(str(failure))
        if failures:
            raise ValueError(
                f"Operation failed: {error}. Recovery incomplete: {'; '.join(failures)}. "
                f"Keep {self.journal} and backups for recovery from the host console."
            ) from error
        if previous:
            self.save(previous)
        else:
            self.journal.unlink(missing_ok=True)
        raise ValueError(
            f"Operation failed; previous files and live state restored: {error}"
        ) from error

    def install(self):
        bundle = self.bundle
        with self.locked():
            previous = self.load()
            if previous:
                for key in ("devices", "addresses", "routes"):
                    if previous["config"][key] != bundle.config[key]:
                        raise ValueError(
                            "Router network layout changed; uninstall the current "
                            "installation before applying a different layout."
                        )
            before = self.snapshot(bundle.options)
            if previous:
                if previous["baseline"]["network"].get("config") != before[
                    "network"
                ].get("config"):
                    raise ValueError(
                        "Router network or DNS configuration changed; uninstall before applying a different layout"
                    )
                self.check_drift(previous, self.snapshot(previous["options"]))
            self.network.preflight(before["network"])
            self.system.preflight(before["system"], bundle.options)
            baseline = deepcopy(before if previous is None else previous["baseline"])
            if previous:
                # Capture newly selected monitoring resources before first use.
                for section in ("units", "tables"):
                    for name, state in before["system"].get(section, {}).items():
                        baseline["system"].setdefault(section, {}).setdefault(
                            name, state
                        )
                baseline["system"]["options"] = before["system"]["options"]
            paths = list(dict.fromkeys([*bundle.changes, bundle.manifest]))
            checkpoint = self.checkpoint(paths, bundle.directory_changes(paths))
            self.save_runtime(checkpoint, before)
            self.archive_journal(checkpoint)
            record = {
                "format": 1,
                "phase": "applying",
                "config": bundle.config,
                "options": bundle.options,
                "baseline": baseline,
                "before": before,
                "checkpoint": str(checkpoint[0]),
            }
            self.save(record)
            try:
                bundle.install_files()
                self.system.prepare(bundle.options, before["system"])
                self.network.apply(before["network"])
                self.system.apply(bundle.options, before["system"])
                self.network.verify()
                self.system.verify(bundle.options)
                record["applied"] = self.snapshot(bundle.options)
                record["phase"] = "active"
                record.pop("before")
                self.save(record)
                self.archive_journal(checkpoint)
            except BaseException as error:  # noqa: BLE001 - roll back interrupted mutations
                self.recover(checkpoint, before, bundle.options, previous, error)
        print("PASS router settings applied and selected services enabled and running.")
        print("Run bash test.sh to check connectivity to VPN, ForcAD and vulnboxes.")

    def uninstall(self, force=False, dry_run=False):
        bundle = self.bundle
        with self.locked():
            previous = self.load()
            plan = bundle.preflight_uninstall(force=force)
            if previous is None:
                bundle.uninstall_files(force=force, dry_run=dry_run)
                print(
                    "No recorded live installation; manually applied settings are unchanged."
                )
                return
            if not plan["installed"]:
                raise ValueError(
                    "Live installation has no file manifest; recover its saved files first"
                )
            options = previous["options"]
            before = self.snapshot(
                options, previous["baseline"]["network"].get("config")
            )
            self.check_drift(previous, before, force=force)
            self.system.preflight(before["system"], options)
            if dry_run:
                bundle.uninstall_files(force=force, dry_run=True)
                print(
                    "Would restore recorded router addresses, routes, sysctls, DNS, rules and services."
                )
                return
            paths = [bundle.path(item["path"]) for item in plan["actions"]]
            checkpoint = self.checkpoint(paths, plan["directories"])
            self.save_runtime(checkpoint, before)
            self.archive_journal(checkpoint)
            self.save(
                {
                    **previous,
                    "phase": "uninstalling",
                    "before": before,
                    "checkpoint": str(checkpoint[0]),
                }
            )
            try:
                self.system.stop(options)
                bundle.uninstall_files(force=force)
                self.network.restore(
                    previous["baseline"]["network"],
                    applied=previous["applied"]["network"],
                    force=force,
                )
                self.system.restore(previous["baseline"]["system"])
                self.journal.unlink()
            except BaseException as error:  # noqa: BLE001 - roll back interrupted mutations
                self.recover(
                    checkpoint,
                    before,
                    options,
                    previous,
                    error,
                    allowed_current=previous["baseline"]["network"],
                )
        print("PASS original router files and live state restored. Backups retained.")

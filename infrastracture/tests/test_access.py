"""Operator access keeps private files private and rollback metadata intact."""

import json
import os
import stat
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import test_host

from hostlib import access


class AccessTests(unittest.TestCase):
    make = test_host.HostTests.make
    write = test_host.HostTests.write
    vpn_configs = test_host.HostTests.vpn_configs

    def setUp(self):
        test_host.HostTests.setUp(self)
        uid = os.getuid() or 65534
        gid = os.getgid() or 65534
        self.account = SimpleNamespace(pw_uid=uid, pw_gid=gid)
        self.identity = SimpleNamespace(
            pw_uid=self.account.pw_uid, pw_gid=self.account.pw_gid
        )
        self.environment = patch.dict(
            os.environ,
            SUDO_USER="organizator",
            SUDO_UID=str(self.account.pw_uid),
        )
        self.environment.start()
        self.addCleanup(self.environment.stop)
        lookup = patch.object(access.pwd, "getpwnam", return_value=self.identity)
        self.lookup = lookup.start()
        self.addCleanup(lookup.stop)

    def test_vpn_operator_owns_private_configs_and_directories(self):
        installer, _ = self.vpn_configs()
        wireguard = self.root / "etc/wireguard"
        wireguard.mkdir(parents=True)
        wireguard.chmod(0o750)
        unrelated = wireguard / "other.conf"
        unrelated.write_text("existing tunnel\n")
        unrelated.chmod(0o640)
        installer.plan()
        installer.install()
        self.assertEqual(stat.S_IMODE(wireguard.stat().st_mode), 0o700)
        installed = json.loads(installer.manifest.read_text())
        self.assertEqual(installed["owner"]["name"], "organizator")
        for name in ("wg-admin", "wg-attack"):
            path = wireguard / f"{name}.conf"
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.assertEqual(path.stat().st_uid, self.account.pw_uid)
            record = next(
                item
                for item in installed["files"]
                if item["path"].endswith(f"/{name}.conf")
            )
            self.assertEqual(record["uid"], self.account.pw_uid)
        installer.uninstall()
        self.assertEqual(stat.S_IMODE(wireguard.stat().st_mode), 0o750)
        self.assertEqual(unrelated.read_text(), "existing tunnel\n")
        self.assertEqual(stat.S_IMODE(unrelated.stat().st_mode), 0o640)

    def test_reinstall_keeps_the_first_operator(self):
        installer = self.make("router")
        installer.plan()
        installer.install()
        with patch.dict(os.environ, SUDO_USER="different", SUDO_UID="9999"):
            repeated = self.make("router")
            repeated.plan()
            repeated.install()
        self.assertEqual(
            json.loads(repeated.manifest.read_text())["owner"]["name"], "organizator"
        )
        self.assertNotIn(
            ("different",), [call.args for call in self.lookup.call_args_list]
        )

    def test_checkout_grants_owner_write_and_preserves_secrets_and_service_data(self):
        target = self.work / "checkout"
        private = target / "config/runtime"
        private.mkdir(parents=True, mode=0o700)
        (target / ".forcad-fresh-install").write_text("fixture\n")
        credentials = private / "admin.env"
        credentials.write_text("fixture\n")
        credentials.chmod(0o600)
        script = target / "control.sh"
        script.write_text("fixture\n")
        script.chmod(0o555)
        database = target / "docker_volumes/postgres/data"
        database.mkdir(parents=True, mode=0o700)
        data = database / "PG_VERSION"
        data.write_text("18\n")
        data.chmod(0o400)
        outside = self.work / "outside"
        outside.write_text("keep\n")
        outside.chmod(0o400)
        (target / "link").symlink_to(outside)
        access.checkout(target, access.operator())
        self.assertEqual(stat.S_IMODE(credentials.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(private.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(script.stat().st_mode), 0o755)
        self.assertEqual(stat.S_IMODE(data.stat().st_mode), 0o400)
        self.assertEqual(stat.S_IMODE(database.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(outside.stat().st_mode), 0o400)

    def test_invalid_or_reassigned_operator_is_rejected(self):
        with (
            patch.dict(os.environ, SUDO_UID="9999"),
            self.assertRaisesRegex(ValueError, "different users"),
        ):
            access.operator()
        with self.assertRaisesRegex(ValueError, "identity changed"):
            access.operator({"name": "organizator", "uid": 9999, "gid": 9999})

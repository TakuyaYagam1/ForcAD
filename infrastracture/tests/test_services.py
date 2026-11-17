"""Stop managed firewalls without propagating shutdown to networking."""

import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from hostlib import services


class ServiceTests(unittest.TestCase):
    def setUp(self):
        scratch = tempfile.TemporaryDirectory()
        self.addCleanup(scratch.cleanup)
        self.root = Path(scratch.name)
        self.bundle = SimpleNamespace(path=lambda name: self.root / name.lstrip("/"))
        self.path = self.bundle.path(
            "/etc/systemd/system/networking.service.d/forcad.conf"
        )
        self.path.parent.mkdir(parents=True)

    def exercise(self, role, failure=False, foreign=False):
        firewall = f"forcad-{role}-firewall.service"
        content = f"[Unit]\nRequires={firewall}\nAfter={firewall}\n".encode()
        self.path.write_bytes(content)
        self.path.chmod(0o640)
        state = {"requires": True, "networking": True, "stopped": False}

        def run(*args):
            if args == ("systemctl", "daemon-reload"):
                state["requires"] = self.path.exists() or foreign
                return ""
            if args[:2] == ("systemctl", "show"):
                return firewall if state["requires"] else ""
            if args == ("systemctl", "stop", firewall):
                state["stopped"] = True
                if state["requires"]:
                    state["networking"] = False
                if failure:
                    raise ValueError("stop failed")
                return ""
            raise AssertionError(args)

        with patch.object(services, "run", side_effect=run):
            if failure or foreign:
                with self.assertRaisesRegex(ValueError, "stop failed|still requires"):
                    services.stop_firewall(self.bundle, firewall)
            else:
                services.stop_firewall(self.bundle, firewall)
        self.assertTrue(state["networking"])
        self.assertEqual(state["stopped"], not foreign)
        self.assertEqual(self.path.read_bytes(), content)
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o640)
        self.assertTrue(state["requires"])

    def test_router_stop_preserves_network_and_restores_boot_dependency(self):
        self.exercise("router")

    def test_vpn_stop_preserves_network_and_restores_boot_dependency(self):
        self.exercise("vpn")

    def test_failed_stop_restores_dropin(self):
        self.exercise("router", failure=True)

    def test_dependency_from_another_file_prevents_stop(self):
        self.exercise("router", foreign=True)

    def test_custom_dropin_is_not_changed(self):
        self.path.write_text("[Unit]\nRequires=custom.service\n")
        with (
            patch.object(services, "run") as run,
            self.assertRaisesRegex(ValueError, "custom networking"),
        ):
            services.stop_firewall(self.bundle, "forcad-router-firewall.service")
        run.assert_not_called()
        self.assertEqual(self.path.read_text(), "[Unit]\nRequires=custom.service\n")

    def test_absent_dropin_is_not_created(self):
        with patch.object(services, "run", return_value="") as run:
            services.stop_firewall(self.bundle, "forcad-router-firewall.service")
        self.assertEqual(run.call_args_list[0].args, ("systemctl", "daemon-reload"))
        run.assert_any_call("systemctl", "stop", "forcad-router-firewall.service")
        self.assertFalse(self.path.exists())


if __name__ == "__main__":
    unittest.main()

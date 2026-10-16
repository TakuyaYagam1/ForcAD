"""VPN runtime install and uninstall behavior with an isolated host root."""

import base64
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from helpers import small_config, source_bundle

import render


class VpnLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        work = Path(self.scratch.name)
        self.root = work / "host"
        self.root.mkdir()
        self.bundle_dir = work / "bundle"
        config = render.load_config(
            Path(render.__file__).parent / "settings.example.toml"
        )
        small_config(config)
        render.write_bundle(self.bundle_dir, render.render(config))
        server = self.bundle_dir / "vpn/server"
        server.mkdir()
        self.original = self.root / "etc/network/interfaces"
        self.original.parent.mkdir(parents=True)
        self.original.write_text("auto lo\niface lo inet loopback\n")
        self.make()

    def make(self):
        bundle = source_bundle("vpn", self.bundle_dir / "vpn", self.root)
        bundle.files_only = False
        server = self.bundle_dir / "vpn/server"
        for name, expected in bundle.config["wireguard"].items():
            private = base64.b64encode(bytes(range(32))).decode()
            content = (
                f"[Interface]\nPrivateKey = {private}\n"
                f"Address = {expected['address']}\nListenPort = {expected['port']}\n"
            )
            for number, peer in enumerate(expected["peers"]):
                public = base64.b64encode(bytes([number + 1]) * 32).decode()
                content += f"\n[Peer]\nPublicKey = {public}\nAllowedIPs = {peer}\n"
            (server / f"{name}.conf").write_text(content)
        bundle.plan()
        return bundle

    @staticmethod
    def snapshots():
        before = {
            "role": "vpn",
            "units": {"baseline": "old"},
            "table": None,
            "sysctls": {"net.ipv4.ip_forward": "0"},
            "network": {"addresses": ["old"]},
        }
        applied = {
            "role": "vpn",
            "units": {"baseline": "running"},
            "table": "forcad_vpn",
            "sysctls": {"net.ipv4.ip_forward": "1"},
            "network": {"addresses": ["installed"]},
        }
        return before, applied

    def bind_runtime(self, bundle, snapshots):
        from vpnlib.lifecycle import Lifecycle

        lifecycle = Lifecycle(bundle)
        runtime = Mock()
        runtime.snapshot.side_effect = snapshots
        lifecycle.runtime = runtime
        patcher = patch("vpn.host.Lifecycle", return_value=lifecycle)
        patcher.start()
        self.addCleanup(patcher.stop)
        return lifecycle, runtime

    def test_install_applies_runtime_and_uninstall_restores_it(self):
        bundle = self.make()
        before, applied = self.snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before, applied])

        bundle.install()

        runtime.preflight.assert_called_once_with(before, allow_existing_table=False)
        runtime.apply.assert_called_once_with(before)
        self.assertEqual(lifecycle.load()["baseline"], before)
        self.assertTrue(bundle.manifest.exists())

        runtime.snapshot.side_effect = None
        runtime.snapshot.return_value = applied
        bundle.uninstall()

        runtime.stop.assert_called_once()
        runtime.restore.assert_called_once_with(before)
        self.assertFalse(bundle.manifest.exists())
        self.assertIsNone(lifecycle.load())

    def test_runtime_start_failure_restores_preexisting_staged_files(self):
        bundle = self.make()
        bundle.files_only = True
        bundle.install()
        installed_interfaces = bundle.path("/etc/network/interfaces").read_bytes()
        manifest = bundle.manifest.read_bytes()
        bundle.files_only = False
        before, _ = self.snapshots()
        lifecycle, runtime = self.bind_runtime(bundle, [before])
        runtime.apply.side_effect = ValueError("tunnel failed")

        with self.assertRaisesRegex(ValueError, "files and live state restored"):
            bundle.install()

        self.assertEqual(
            bundle.path("/etc/network/interfaces").read_bytes(), installed_interfaces
        )
        self.assertEqual(bundle.manifest.read_bytes(), manifest)
        runtime.restore.assert_called_once_with(before)
        self.assertIsNone(lifecycle.load())


if __name__ == "__main__":
    unittest.main()

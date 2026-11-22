"""Boundary checks for installing monitoring on an already running deployment."""

import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from helpers import ROOT, source_bundle

import render


class MonitoringAddons(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.bundle_dir = self.root / 'bundle'
        self.bundle_dir.mkdir()
        self.config = render.load_config(ROOT / 'settings.example.toml')

    def test_targeted_transaction_preserves_unrelated_manifest_and_local_edits(self):
        (self.bundle_dir / 'host.json').write_text('{"role":"forcad"}')
        bundle = source_bundle('forcad', self.bundle_dir, self.root)
        bundle.options = {'repo': '/opt/game', 'network_metrics': False, 'game_metrics': False}
        bundle.put('/opt/game/config', b'original')
        bundle.install_files()
        original = json.loads(bundle.manifest.read_text())['files'][0]
        self.root.joinpath('opt/game/config').write_bytes(b'operator edit')
        bundle.changes.clear()
        bundle.put('/opt/game/dashboard', b'new')
        bundle.install_files(extend=True)
        self.assertEqual(self.root.joinpath('opt/game/config').read_bytes(), b'operator edit')
        self.assertIn(original, json.loads(bundle.manifest.read_text())['files'])
        self.assertEqual(bundle.preflight_uninstall(force=True)['installed'], True)

    def test_new_secrets_only_when_missing_and_no_target_clobber(self):
        from forcad.monitoring import credentials

        original = b'# operator settings\nGRAFANA_ADMIN_PASSWORD="keep"\nFOO=unchanged\n'
        updated = credentials(original)
        self.assertTrue(updated.startswith(original))
        self.assertRegex(updated, rb'GRAFANA_CHECKER_DB_PASSWORD="[0-9a-f]{64}"\n')
        self.assertEqual(credentials(updated), updated)
        with self.assertRaises(ValueError):
            credentials(original + b'GRAFANA_ADMIN_PASSWORD="other"\n')

    def test_managed_install_rejects_wrong_inventory_before_any_write(self):
        from forcad.monitoring import prepare

        (self.bundle_dir / 'host.json').write_text('{"role":"forcad","teams":[]}')
        bundle = source_bundle('forcad', self.bundle_dir, self.root)
        repo = '/opt/game'
        bundle.options = {'repo': repo, 'game_metrics': False, 'network_metrics': False}
        bundle.put(f'{repo}/.forcad-fresh-install', b'https://example.org/game main\n')
        bundle.install_files()
        bundle.config['teams'] = [{'name': 'Different deployment'}]
        bundle.changes.clear()
        with self.assertRaisesRegex(ValueError, 'inventory'):
            prepare(bundle, repo, 'https://example.org/game')
        self.assertFalse(bundle.changes)

    def test_enabling_existing_checkout_preserves_credentials_discovery_and_game(self):
        from forcad.monitoring import ASSETS, prepare

        repo = '/opt/game'
        url = 'https://example.org/game'
        assets = render.monitoring_assets()
        render.write_bundle(self.bundle_dir / 'release', {
            'host.json': json.dumps({'role': 'forcad', 'teams': [], 'checks': {'metrics_address': '10.0.0.1'}}),
            **{k.removeprefix('forcad/'): v for k, v in assets.items()},
        })
        bundle = source_bundle('forcad', self.bundle_dir / 'release', self.root)
        bundle.options = {'repo': repo, 'network_metrics': False, 'game_metrics': False}
        bundle.put(f'{repo}/config/config.yml', b'game settings')
        bundle.install_files()
        project = self.root / 'opt/game'
        env = b'GRAFANA_ADMIN_PASSWORD="my-existing-secret"\nCUSTOM=preserve\n'
        fixture = {
            '.forcad-monitoring': b'false\n',
            '.forcad-fresh-install': (url + ' main\nmonitoring_sha256=' + hashlib.sha256(b'false\n').hexdigest() + '\n').encode(),
            'deployment/docker/.env': env,
            'deployment/monitoring/file_sd/routers.yml': b'- targets: ["10.0.0.8:9109"]\n',
            'backend/bin/forcad': b'',
            'deployment/docker/compose.monitoring.yml': b'',
            'deployment/monitoring/grafana/setup_checker_reader.sh': b'',
            'deployment/monitoring/grafana/provisioning/datasources/checker.yml': b'',
        }
        for name, value in fixture.items():
            path = project / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(value)
        bundle.changes.clear()
        prepare(bundle, repo, url)
        bundle.install_files(extend=True)
        after = (project / 'deployment/docker/.env').read_bytes()
        self.assertTrue(after.startswith(env))
        self.assertEqual((project / 'config/config.yml').read_bytes(), b'game settings')
        self.assertEqual((project / '.forcad-monitoring').read_bytes(), b'true\n')
        self.assertIn(hashlib.sha256(b'true\n').hexdigest(), (project / '.forcad-fresh-install').read_text())
        for target in ('10.0.0.8:9109', '10.0.0.1:9109'):
            self.assertIn(target, (project / 'deployment/monitoring/file_sd/routers.yml').read_text())
        for name in ASSETS:
            self.assertTrue((project / 'deployment/monitoring' / name).is_file())
        bundle.changes.clear()
        manifest = bundle.manifest.read_bytes()
        prepare(bundle, repo, url)
        bundle.install_files(extend=True)
        self.assertEqual(bundle.manifest.read_bytes(), manifest)
        self.assertEqual((project / 'deployment/docker/.env').read_bytes(), after)
        local = project / 'deployment/monitoring/grafana/dashboards/relay.json'
        local.write_bytes(b'operator-edited dashboard')
        bundle.changes.clear()
        with self.assertRaisesRegex(ValueError, 'Locally modified'):
            prepare(bundle, repo, url)
        self.assertEqual(local.read_bytes(), b'operator-edited dashboard')
        self.assertEqual(bundle.manifest.read_bytes(), manifest)

    def test_monitoring_uses_deployed_inventory_without_original_asset_files(self):
        settings = self.root / "event.toml"
        example = (ROOT / "settings.example.toml").read_text()
        settings.write_text(example.replace('"danosito.jpg"', '"archived-team-logo.png"'))
        with self.assertRaisesRegex(ValueError, "regular file"):
            render.load_config(settings)
        config = render.load_config(settings, require_assets=False)
        self.assertIn("router/network.json", render.monitoring_bundle(config))
        settings.write_text(example.replace('"danosito.jpg"', '"../escape"'))
        with self.assertRaisesRegex(ValueError, "safe relative path"):
            render.load_config(settings, require_assets=False)

    def test_monitoring_bundle_contains_evidence_ui_not_game_secrets(self):
        data = render.monitoring_bundle(self.config)
        self.assertIn('vpn/vpnlib/history.py', data)
        self.assertIn('forcad/monitoring/grafana/dashboards/relay.json', data)
        self.assertIn('router/game-metrics.zeek', data)
        self.assertFalse(any(k in data for k in ['forcad/.env', 'forcad/config.yml', 'settings.toml']))
        self.assertFalse(any('wireguard/' in key or '/server/' in key for key in data))
        self.assertNotIn(b'PrivateKey', data['vpn/host.json'].encode())

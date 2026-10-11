"""Installed-package contracts shared by the CLI and container entrypoints."""

import os
from importlib.resources import files
from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory
from unittest import TestCase
from unittest.mock import patch

import yaml

from forcad import entrypoints
from forcad.cli import constants
from forcad.cli.base.setup import prepare_compose


class PackagingTests(TestCase):
    def test_source_compose_paths_match_the_deployment_layout(self):
        root = Path(__file__).resolve().parents[2]
        self.assertEqual(constants.DEPLOYMENT_DIR, root / 'deployment/docker')
        self.assertEqual(
            constants.FULL_COMPOSE_PATH,
            root / 'deployment/docker/compose.yml',
        )
        self.assertEqual(
            constants.TESTS_COMPOSE_PATH,
            root / 'deployment/docker/compose.tests.yml',
        )
        self.assertTrue(constants.FULL_COMPOSE_PATH.is_file())
        self.assertTrue(constants.TESTS_COMPOSE_PATH.is_file())

    def test_project_root_discovery_finds_compose_under_deployment_docker(self):
        source = Path(__file__).resolve().parents[1] / 'src'
        with TemporaryDirectory() as directory:
            root = Path(directory)
            compose_dir = root / 'deployment/docker'
            compose_dir.mkdir(parents=True)
            (compose_dir / 'compose.yml').write_text('services: {}\n')
            environment = os.environ.copy()
            environment.pop('FORCAD_ROOT', None)
            environment['PYTHONPATH'] = str(source)
            result = subprocess.run(
                [sys.executable, '-c',
                 'from forcad.paths import resolve_project_root; '
                 'print(resolve_project_root())'],
                cwd=root,
                env=environment,
                check=True,
                capture_output=True,
                text=True,
                timeout=10,
            )
        self.assertEqual(Path(result.stdout.strip()), root.resolve())

    def test_external_storages_preserve_the_canonical_compose(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            base = root / 'compose.yml'
            generated = root / 'runtime/compose.external.yml'
            source = yaml.safe_dump({'services': {
                'postgres': {}, 'postgres-data': {}, 'redis': {},
                'rabbitmq': {}, 'celery': {},
            }})
            base.write_text(source)
            with patch.object(constants, 'FULL_COMPOSE_PATH', base), \
                    patch.object(constants, 'GENERATED_COMPOSE_PATH', generated), \
                    patch.object(constants, 'RUNTIME_DIR', generated.parent):
                prepare_compose(redis='cache', database='db', rabbitmq='broker')
                external = yaml.safe_load(generated.read_text())
                self.assertEqual(set(external['services']), {'celery'})
                self.assertEqual(base.read_text(), source)
                prepare_compose(redis=None, database=None, rabbitmq=None)
                self.assertFalse(generated.exists())
                self.assertEqual(base.read_text(), source)

    def test_runtime_commands_stay_in_the_platform_environment(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            platform_bin = root / 'platform/bin'
            checker_bin = root / 'checkers/bin'
            platform_bin.mkdir(parents=True)
            checker_bin.mkdir(parents=True)
            interpreter = platform_bin / 'python'
            interpreter.symlink_to(sys.executable)
            for name in ('gunicorn', 'celery'):
                for location in (platform_bin, checker_bin):
                    executable = location / name
                    executable.write_text('#!/bin/sh\nexit 0\n')
                    executable.chmod(0o755)
            with patch.object(sys, 'executable', str(interpreter)), \
                    patch.dict(os.environ, {'PATH': str(checker_bin)}, clear=True):
                web = entrypoints.command_for('api', ['--timeout', '60'])
                self.assertEqual(web[0], str(platform_bin / 'gunicorn'))
                self.assertIn('forcad.services.api.app:app', web)
                self.assertEqual(web[web.index('--bind') + 1], '0.0.0.0:5000')
                self.assertEqual(web[web.index('--worker-class') + 1], 'gthread')
                self.assertEqual(web[web.index('--threads') + 1], '100')
                self.assertEqual(web[web.index('--timeout') + 1], '60')

                worker = entrypoints.command_for('worker', [])
                self.assertEqual(worker[0], str(platform_bin / 'celery'))
                self.assertIn('forcad.services.tasks.app:celery_app', worker)
                self.assertIn('worker', worker)
                self.assertIn('-E', worker)
                self.assertEqual(worker[worker.index('--autoscale') + 1], '32,16')

    def test_database_scripts_are_package_resources(self):
        resources = files('forcad.scripts')
        for name in ('create_tables.sql', 'create_functions.sql',
                     'create_dispatch.sql', 'create_sessions.sql',
                     'drop_query.sql'):
            with self.subTest(name=name):
                self.assertTrue(resources.joinpath(name).read_text().strip())

    def test_operator_cli_help_works_outside_the_checkout(self):
        environment = os.environ.copy()
        environment.pop('PYTHONPATH', None)
        with TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'deployment/docker').mkdir(parents=True)
            (root / 'deployment/docker/compose.yml').write_text(
                'services: {}\n'
            )
            environment['FORCAD_ROOT'] = directory
            result = subprocess.run(
                [sys.executable, '-c', 'from forcad.cli import cli; cli()',
                 '--help'],
                cwd=directory,
                env=environment,
                check=True,
                capture_output=True,
                text=True,
                timeout=10,
            )
        for command in ('setup', 'start', 'reset', 'clean'):
            self.assertIn(command, result.stdout)

    def test_runtime_help_does_not_require_storage_configuration(self):
        environment = {'PATH': os.environ.get('PATH', '')}
        with TemporaryDirectory() as directory:
            result = subprocess.run(
                [sys.executable, '-m', 'forcad.entrypoints', '--help'],
                cwd=directory,
                env=environment,
                check=True,
                capture_output=True,
                text=True,
                timeout=10,
            )
        for role in ('api', 'http_receiver', 'worker', 'ticker'):
            self.assertIn(role, result.stdout)

import json
import os
import shutil
import subprocess
import sys
from contextlib import ExitStack
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import TestCase
from unittest.mock import MagicMock, patch

import click
from click.testing import CliRunner
import psycopg2
import redis

PROJECT_DIR = Path(__file__).resolve().parents[1]
sys.path[:0] = [str(PROJECT_DIR), str(PROJECT_DIR / 'backend')]

from cli import cli, cleanup, constants
from scripts import reset_db


class CleanupTestCase(TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(TemporaryDirectory()))
        self.stack.enter_context(patch.object(constants, 'BASE_DIR', self.root))
        self.stack.enter_context(patch.dict(os.environ, {}, clear=True))
        self.generated = [self.root / constants.BASE_COMPOSE_FILE]
        for name in ('ADMIN_ENV_PATH', 'POSTGRES_ENV_PATH',
                     'RABBITMQ_ENV_PATH', 'REDIS_ENV_PATH'):
            path = self.root / (name.lower() + '.env')
            self.stack.enter_context(patch.object(constants, name, path))
            self.generated.append(path)
        for path in self.generated:
            path.write_text('generated')
        self.data = self.root / 'docker_volumes/postgres/data'
        self.data.mkdir(parents=True)
        (self.data / 'PG_VERSION').write_text('13')
        (self.root / 'config.yml').write_text('keep config and tokens')
        (self.root / 'checkers').mkdir()
        (self.root / 'checkers/checker.py').write_text('keep checker')
        (self.root / 'docker_volumes/keep.txt').write_text('unrelated')
        self.config = {'name': 'forcad-cleanup-test', 'services': {
            'postgres': {'image': 'postgres:13.4-alpine', 'volumes': [{
                'type': 'bind', 'source': str(self.data),
                'target': '/var/lib/postgresql/data',
            }]},
            'redis': {}, 'rabbitmq': {}, 'initializer': {}, 'ticker': {},
        }}
        self.commands = []
        self.execute = self.stack.enter_context(patch.object(
            cleanup, 'execute', side_effect=self.run_command,
        ))
        self.runner = CliRunner()

    def run_command(self, command, **kwargs):
        self.commands.append(command)
        if 'config' in command:
            return json.dumps(self.config)
        if command[:3] == ['docker', 'context', 'inspect']:
            return 'unix:///var/run/docker.sock'
        if command[:3] == ['docker', 'image', 'inspect']:
            return 'sha256:test-image'
        if command[:3] == ['docker', 'ps', '-aq']:
            return ''
        if command[-1] == cleanup.DELETE_DATA:
            shutil.rmtree(self.data)
        return ''

    def invoke(self, *args):
        result = self.runner.invoke(cli, args)
        self.assertEqual(result.exit_code, 0, result.output)
        return result

    def assert_preserved(self):
        for path, expected in {
            'config.yml': 'keep config and tokens',
            'checkers/checker.py': 'keep checker',
            'docker_volumes/keep.txt': 'unrelated',
        }.items():
            self.assertEqual((self.root / path).read_text(), expected)

    def test_reset_stops_writers_before_running_database_script(self):
        self.invoke('reset')
        stop = next(i for i, c in enumerate(self.commands) if 'stop' in c)
        up = next(i for i, c in enumerate(self.commands) if 'up' in c)
        run = next(i for i, c in enumerate(self.commands) if 'initializer' in c)
        down = next(i for i, c in enumerate(self.commands) if 'down' in c)
        self.assertLess(stop, up)
        self.assertLess(up, run)
        self.assertLess(run, down)
        self.assertIn('--rm', self.commands[run])
        self.assertIn('--no-deps', self.commands[run])
        self.assertIn('--entrypoint', self.commands[run])
        self.assertTrue(self.data.exists())
        self.assertTrue(all(p.exists() for p in self.generated))
        self.assert_preserved()

    def test_reset_failure_does_not_report_success_or_remove_config(self):
        def fail(command, **kwargs):
            if 'initializer' in command:
                raise click.ClickException('database failure')
            return self.run_command(command, **kwargs)
        self.execute.side_effect = fail
        result = self.runner.invoke(cli, ['reset'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertIn('reset --full', result.output)
        self.assertNotIn('Done!', result.output)
        self.assertFalse(any('down' in c for c in self.commands))
        self.assertTrue(all(p.exists() for p in self.generated))

    def test_full_reset_removes_only_database_after_down(self):
        self.invoke('reset', '--full')
        down = next(i for i, c in enumerate(self.commands) if 'down' in c)
        delete = next(i for i, c in enumerate(self.commands)
                      if c[-1] == cleanup.DELETE_DATA)
        self.assertLess(down, delete)
        self.assertFalse(any('initializer' in c for c in self.commands))
        self.assertIn('--pull=never', self.commands[delete])
        self.assertIn('--network=none', self.commands[delete])
        self.assertFalse(self.data.exists())
        self.assertTrue(all(p.exists() for p in self.generated))
        self.assert_preserved()

    def test_full_reset_is_repeatable(self):
        self.invoke('reset', '--full')
        self.invoke('reset', '--full')
        self.assert_preserved()

    def test_full_reset_accepts_trailing_slash_in_mount(self):
        mount = self.config['services']['postgres']['volumes'][0]
        mount['target'] += '/'
        mount['source'] += '/'
        self.invoke('reset', '--full')
        self.assertFalse(self.data.exists())

    def test_clean_removes_generated_config_after_full_reset(self):
        self.invoke('clean')
        self.assertFalse(self.data.exists())
        self.assertTrue(all(not p.exists() for p in self.generated))
        self.assert_preserved()
        self.invoke('clean')

    def test_failed_down_preserves_database_and_config(self):
        def fail(command, **kwargs):
            if 'down' in command:
                raise click.ClickException('down failed')
            return self.run_command(command, **kwargs)
        self.execute.side_effect = fail
        result = self.runner.invoke(cli, ['clean'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertNotIn('Cleanup successful!', result.output)
        self.assertTrue(self.data.exists())
        self.assertTrue(all(p.exists() for p in self.generated))

    def test_clean_rejects_symlinked_config_before_deleting_data(self):
        self.generated[1].unlink()
        self.generated[1].symlink_to(self.root / 'config.yml')
        result = self.runner.invoke(cli, ['clean'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertFalse(self.commands)
        self.assertTrue(self.data.exists())

    def test_reset_refuses_project_from_another_directory(self):
        def foreign_project(command, **kwargs):
            if command[:3] == ['docker', 'ps', '-aq']:
                return 'container-id'
            if command[:2] == ['docker', 'inspect']:
                return json.dumps({'com.docker.compose.project.working_dir': '/other'})
            return self.run_command(command, **kwargs)
        self.execute.side_effect = foreign_project
        result = self.runner.invoke(cli, ['reset', '--full'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertFalse(any('down' in c or 'stop' in c for c in self.commands))
        self.assertTrue(self.data.exists())

    def test_failed_removal_preserves_generated_config(self):
        def fail(command, **kwargs):
            if command[-1] == cleanup.DELETE_DATA:
                raise click.ClickException('permission denied')
            return self.run_command(command, **kwargs)
        self.execute.side_effect = fail
        result = self.runner.invoke(cli, ['clean'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertTrue(all(p.exists() for p in self.generated))

    def test_full_reset_rejects_external_storage_before_stop(self):
        del self.config['services']['postgres']
        result = self.runner.invoke(cli, ['reset', '--full'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertFalse(any('down' in c or 'stop' in c for c in self.commands))

    def test_normal_reset_supports_external_storage(self):
        for name in cleanup.STORAGES:
            del self.config['services'][name]
        self.invoke('reset')
        self.assertFalse(any('up' in c for c in self.commands))
        self.assertTrue(any('initializer' in c for c in self.commands))

    def test_full_reset_rejects_nonstandard_mount(self):
        self.config['services']['postgres']['volumes'][0]['source'] = '/other/data'
        result = self.runner.invoke(cli, ['reset', '--full'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertFalse(any('down' in c for c in self.commands))

    def test_full_reset_rejects_remote_docker(self):
        with patch.dict(os.environ, {'DOCKER_HOST': 'ssh://remote'}):
            result = self.runner.invoke(cli, ['reset', '--full'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertFalse(any('down' in c for c in self.commands))

    def test_full_reset_rejects_symlinked_directory(self):
        shutil.rmtree(self.data)
        self.data.symlink_to(self.root / 'checkers', target_is_directory=True)
        result = self.runner.invoke(cli, ['reset', '--full'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertFalse(any('down' in c for c in self.commands))
        self.assert_preserved()

    def test_missing_setup_with_existing_data_fails(self):
        self.generated[0].unlink()
        result = self.runner.invoke(cli, ['clean'])
        self.assertNotEqual(result.exit_code, 0)
        self.assertTrue(self.data.exists())

    def test_fast_and_test_overrides(self):
        self.invoke('reset', '--fast')
        self.assertIn(constants.FAST_COMPOSE_FILE, self.commands[0])
        self.commands.clear()
        with patch.dict(os.environ, {'TEST': '1'}):
            self.invoke('reset')
        self.assertIn(constants.TESTS_COMPOSE_FILE, self.commands[0])

    def test_unlink_error_is_not_hidden(self):
        with patch.object(Path, 'unlink', side_effect=PermissionError(13, 'denied')):
            with self.assertRaises(click.ClickException):
                cleanup.remove_generated_config()


class CommandFailureTestCase(TestCase):
    def test_command_failures_propagate(self):
        failures = [FileNotFoundError(), subprocess.CalledProcessError(1, ['docker']),
                    subprocess.TimeoutExpired(['docker'], 120)]
        for failure in failures:
            with self.subTest(failure=type(failure).__name__):
                with patch.object(subprocess, 'run', side_effect=failure):
                    with self.assertRaises(click.ClickException):
                        cleanup.execute(['docker', 'version'])


class ResetDatabaseTestCase(TestCase):
    def test_storage_wait_is_bounded(self):
        operation = MagicMock(side_effect=redis.exceptions.ConnectionError())
        with patch.object(reset_db.time, 'sleep') as sleep:
            with self.assertRaisesRegex(RuntimeError, '12 attempts'):
                reset_db.wait_for_storage(operation)
        self.assertEqual(operation.call_count, 12)
        self.assertEqual(sleep.call_count, 11)

    def test_storage_can_recover(self):
        operation = MagicMock(side_effect=[psycopg2.OperationalError(), 'connected'])
        with patch.object(reset_db.time, 'sleep'):
            self.assertEqual(reset_db.wait_for_storage(operation), 'connected')

    def test_sql_failure_closes_connection_without_clearing_cache(self):
        connection = MagicMock()
        cursor = connection.cursor.return_value.__enter__.return_value
        cursor.execute.side_effect = psycopg2.DatabaseError('failed')
        with ExitStack() as stack:
            stack.enter_context(patch.object(reset_db.config, 'get_db_config'))
            stack.enter_context(patch.object(
                reset_db.psycopg2, 'connect', return_value=connection,
            ))
            cache = stack.enter_context(patch.object(reset_db.redis, 'Redis'))
            with self.assertRaises(psycopg2.DatabaseError):
                reset_db.run()
        connection.close.assert_called_once_with()
        cache.assert_not_called()

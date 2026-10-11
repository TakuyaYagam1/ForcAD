"""Opt-in Docker checks: FORCAD_DOCKER_TESTS=1 python -m unittest discover
-s backend/tests -p test_cleanup_integration.py -v. Requires cached PostgreSQL 18.6/Redis 8.4.7 images.
"""

import os
from contextlib import ExitStack
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import TestCase, skipUnless
from unittest.mock import Mock, patch

import psycopg2
import redis
import yaml
from click.testing import CliRunner

PROJECT_DIR = Path(__file__).resolve().parents[2]

from forcad.cli import cli, cleanup, constants
from forcad.scripts import reset_db


@skipUnless(os.getenv('FORCAD_DOCKER_TESTS') == '1', 'Docker checks are opt-in')
class CleanupIntegrationTestCase(TestCase):
    def test_reset_and_clean_with_container_owned_data(self):
        with ExitStack() as stack:
            temporary = stack.enter_context(
                TemporaryDirectory(prefix='forcad-cleanup-'),
            )
            root = Path(temporary)
            (root / 'deployment/docker').mkdir(parents=True)
            for name, value in {
                'BASE_DIR': root,
                'DEPLOYMENT_DIR': root / 'deployment/docker',
                'FULL_COMPOSE_PATH': root / 'deployment/docker/compose.yml',
                'TESTS_COMPOSE_PATH': root / 'deployment/docker/compose.tests.yml',
                'GENERATED_COMPOSE_PATH': root / 'config/runtime/compose.external.yml',
                'RUNTIME_DIR': root / 'config/runtime',
            }.items():
                stack.enter_context(patch.object(constants, name, value))
            environment = dict(os.environ)
            environment.pop('TEST', None)
            environment.pop('COMPOSE_PROJECT_NAME', None)
            stack.enter_context(patch.dict(os.environ, environment, clear=True))
            cleanup.local_docker()
            generated = []
            for name in ('ADMIN_ENV_PATH', 'POSTGRES_ENV_PATH',
                         'RABBITMQ_ENV_PATH', 'REDIS_ENV_PATH'):
                path = root / (name.lower() + '.env')
                path.touch(mode=0o600)
                stack.enter_context(patch.object(constants, name, path))
                generated.append(path)
            data = root / 'docker_volumes/postgres/data'
            data.mkdir(parents=True)
            config = root / 'config.yml'
            config.write_text('teams: [{name: test, token: "0123456789abcdef"}]\n')
            expected_config = config.read_bytes()
            keep = root / 'docker_volumes/keep.txt'
            keep.write_text('keep')
            compose_file = constants.FULL_COMPOSE_PATH
            compose = {
                'name': root.name,
                'services': {
                    'postgres': {
                        'image': (
                            'postgres:18.6-alpine3.24@sha256:'
                            '77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
                        ),
                        'environment': {'POSTGRES_PASSWORD': 'old-test-password'},
                        'volumes': [f'{data}/:/var/lib/postgresql/'],
                        'ports': ['127.0.0.1::5432'],
                        'healthcheck': {
                            'test': ['CMD-SHELL', 'pg_isready -U postgres'],
                            'interval': '1s', 'timeout': '5s', 'retries': 30,
                        },
                    },
                    'redis': {
                        'image': (
                            'redis:8.4.7-alpine@sha256:'
                            '0514fa59e3d84e2f3be66731c5401fc2a85b8ea71b49c0116c850f6a558076f4'
                        ),
                        'ports': ['127.0.0.1::6379'],
                        'healthcheck': {
                            'test': ['CMD', 'redis-cli', 'ping'],
                            'interval': '1s', 'timeout': '5s', 'retries': 30,
                        },
                    },
                    # Full cleanup requires all built-in storage services in config.
                    # The broker is never started by this storage test.
                    'rabbitmq': {
                        'image': (
                            'rabbitmq:4.3.6-management-alpine@sha256:'
                            '3ef7f7e89fed709a78f79b8b5f4e30e1312a1d4089da752e23595566d9d61a95'
                        ),
                    },
                },
            }
            compose_file.write_text(yaml.safe_dump(compose))
            postgres = compose['services']['postgres']
            command = cleanup.compose_command(False)

            def docker(*args):
                return cleanup.execute(command + list(args), capture=True)

            def port(service, container_port):
                return int(docker('port', service, container_port).rsplit(':', 1)[1])

            def invoke(*args):
                result = CliRunner().invoke(cli, args)
                self.assertEqual(result.exit_code, 0, result.output)

            try:
                docker('up', '-d', '--pull', 'never', '--wait', 'postgres', 'redis')
                pgdata = data / '18/docker'
                self.assertNotEqual(pgdata.stat().st_uid, os.getuid())
                self.assertTrue((pgdata / 'PG_VERSION').is_file())
                database = dict(host='127.0.0.1', port=port('postgres', '5432'),
                                user='postgres', password='old-test-password',
                                dbname='postgres')
                cache_config = dict(host='127.0.0.1', port=port('redis', '6379'))
                connection = psycopg2.connect(**database, connect_timeout=5)
                cache = redis.Redis(**cache_config, socket_timeout=5)
                try:
                    with connection, connection.cursor() as cursor:
                        cursor.execute('CREATE TABLE Teams (id INTEGER)')
                    cache.set('cleanup-test', 'value')
                    with patch.object(reset_db.config, 'get_db_config',
                                      return_value=Mock(model_dump=lambda: database)), \
                            patch.object(reset_db.config, 'get_redis_config',
                                         return_value=Mock(
                                             model_dump=lambda: cache_config)):
                        reset_db.run()
                    with connection, connection.cursor() as cursor:
                        cursor.execute("SELECT to_regclass('teams')")
                        self.assertIsNone(cursor.fetchone()[0])
                    self.assertEqual(cache.dbsize(), 0)
                finally:
                    connection.close()
                    cache.close()

                postgres['environment']['POSTGRES_PASSWORD'] = 'new-test-password'
                compose_file.write_text(yaml.safe_dump(compose))
                database['password'] = 'new-test-password'
                with self.assertRaises(psycopg2.OperationalError):
                    psycopg2.connect(**database, connect_timeout=5)
                invoke('reset', '--full')
                self.assertFalse(data.exists())
                self.assertEqual(config.read_bytes(), expected_config)
                self.assertTrue(all(path.exists() for path in generated))

                docker('up', '-d', '--pull', 'never', '--wait', 'postgres')
                database['port'] = port('postgres', '5432')
                connection = psycopg2.connect(**database, connect_timeout=5)
                connection.close()
                invoke('clean')
                invoke('clean')
                self.assertFalse(data.exists())
                self.assertTrue(compose_file.exists())
                self.assertTrue(all(not path.exists() for path in generated))
                self.assertEqual(config.read_bytes(), expected_config)
                self.assertEqual(keep.read_text(), 'keep')
            finally:
                # Restore only this temporary fixture's config for teardown.
                compose_file.write_text(yaml.safe_dump(compose))
                docker('down', '--volumes', '--remove-orphans', '--timeout', '10')
                if data.exists():
                    image = cleanup.execute([
                        'docker', 'image', 'inspect', postgres['image'],
                        '--format', '{{.Id}}',
                    ], capture=True)
                    cleanup.execute(cleanup.cleanup_container(image, data, delete=True))

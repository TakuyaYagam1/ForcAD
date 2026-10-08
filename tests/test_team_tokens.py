import copy
import sys
from contextlib import ExitStack
from contextlib import contextmanager
from pathlib import Path
from tempfile import TemporaryDirectory
from types import SimpleNamespace
from unittest import TestCase
from unittest.mock import MagicMock, patch

import yaml
from click.testing import CliRunner
from flask import Flask
from pydantic import ValidationError
from psycopg2.errors import UniqueViolation

PROJECT_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_DIR))
sys.path.insert(0, str(PROJECT_DIR / 'backend'))

from cli import cli, constants
from cli.models import BasicConfig
from lib import models, storage
from lib.helpers import events
from lib.models import Team
from lib.team_logos import TEAM_LOGOS
from scripts import apply_fixes
from scripts.init_db import init_teams
from services.admin.viewsets import admin_bp
from services.admin.viewsets.validation import validate_data


FIXED_TOKEN = '01234567' + '89abcdef'
INVALID_TOKENS = ('', 'a' * 15, 'a' * 17, 'g' * 16, 'A' * 16,
                  FIXED_TOKEN + '\n', 1234567890123456, False)


def game_config():
    config = yaml.safe_load((PROJECT_DIR / 'config.yml.example').read_text())
    config['admin'] = {'username': 'test', 'password': 'test'}
    config['teams'][0]['token'] = FIXED_TOKEN
    return config


class TeamTokenConfigTestCase(TestCase):
    def test_mixed_config_round_trip(self):
        config = BasicConfig.model_validate(game_config(), strict=True)
        restored = yaml.safe_load(yaml.safe_dump(
            config.model_dump(exclude_none=True),
        ))
        self.assertEqual(restored['teams'][0]['token'], FIXED_TOKEN)
        self.assertNotIn('token', restored['teams'][1])

    def test_setup_preserves_tokens(self):
        with TemporaryDirectory() as directory, ExitStack() as stack:
            root = Path(directory)
            config_path = root / 'config.yml'
            config_path.write_text(yaml.safe_dump(game_config()))
            for name, value in {
                'BASE_DIR': root,
                'CONFIG_PATH': config_path,
                'ADMIN_ENV_PATH': root / 'admin.env',
                'POSTGRES_ENV_PATH': root / 'postgres.env',
                'RABBITMQ_ENV_PATH': root / 'rabbitmq.env',
                'REDIS_ENV_PATH': root / 'redis.env',
            }.items():
                stack.enter_context(patch.object(constants, name, value))
            runner = CliRunner()
            for _ in range(2):
                result = runner.invoke(cli, ['setup'])
                self.assertEqual(result.exit_code, 0, result.output)
                config = yaml.safe_load(config_path.read_text())
                self.assertEqual(config['teams'][0]['token'], FIXED_TOKEN)
                self.assertNotIn('token', config['teams'][1])

    def test_invalid_tokens_rejected(self):
        for token in INVALID_TOKENS:
            with self.subTest(token=token):
                config = game_config()
                config['teams'][0]['token'] = token
                with self.assertRaises(ValidationError):
                    BasicConfig.model_validate(config, strict=True)

    def test_duplicate_tokens_rejected(self):
        config = game_config()
        config['teams'][1]['token'] = FIXED_TOKEN
        with self.assertRaises(ValidationError):
            BasicConfig.model_validate(config, strict=True)

    def test_validation_errors_do_not_print_tokens(self):
        config = game_config()
        config['teams'][1]['token'] = FIXED_TOKEN
        with self.assertRaises(ValidationError) as raised:
            BasicConfig.model_validate(config, strict=True)
        self.assertNotIn(FIXED_TOKEN, str(raised.exception))

    def test_null_token_omitted_on_dump(self):
        config = game_config()
        config['teams'][0]['token'] = None
        parsed = BasicConfig.model_validate(config, strict=True)
        self.assertNotIn('token', parsed.model_dump(exclude_none=True)['teams'][0])

    def test_admin_api_requires_cli_token_format(self):
        valid = {
            'name': 'Team',
            'ip': '192.0.2.1',
            'token': FIXED_TOKEN,
        }
        result = validate_data(valid, models.Team)
        self.assertEqual(result['token'], FIXED_TOKEN)

        for token in INVALID_TOKENS:
            with self.subTest(token=token):
                with self.assertRaises(ValueError):
                    validate_data({**valid, 'token': token}, models.Team)

    def test_setup_does_not_print_credentials_and_keeps_storage_passwords(self):
        with TemporaryDirectory() as directory, ExitStack() as stack:
            root = Path(directory)
            config_path = root / 'config.yml'
            config_path.write_text(yaml.safe_dump(game_config()))
            for name, value in {
                'BASE_DIR': root,
                'CONFIG_PATH': config_path,
                'ADMIN_ENV_PATH': root / 'admin.env',
                'POSTGRES_ENV_PATH': root / 'postgres.env',
                'RABBITMQ_ENV_PATH': root / 'rabbitmq.env',
                'REDIS_ENV_PATH': root / 'redis.env',
            }.items():
                stack.enter_context(patch.object(constants, name, value))

            runner = CliRunner()
            first = runner.invoke(cli, ['setup'])
            self.assertEqual(first.exit_code, 0, first.output)
            saved = yaml.safe_load(config_path.read_text())
            storage_passwords = [
                saved['storages']['db']['password'],
                saved['storages']['rabbitmq']['password'],
                saved['storages']['redis']['password'],
            ]
            self.assertEqual(len(set(storage_passwords)), 3)
            self.assertNotIn(saved['admin']['password'], storage_passwords)
            self.assertNotIn(saved['admin']['password'], first.output)
            for password in storage_passwords:
                self.assertNotIn(password, first.output)

            second = runner.invoke(cli, ['setup'])
            self.assertEqual(second.exit_code, 0, second.output)
            resaved = yaml.safe_load(config_path.read_text())
            self.assertEqual(
                [
                    resaved['storages']['db']['password'],
                    resaved['storages']['rabbitmq']['password'],
                    resaved['storages']['redis']['password'],
                ],
                storage_passwords,
            )

    def test_setup_preserves_configured_storage_settings(self):
        config = game_config()
        config['storages'] = {
            'db': {
                'user': 'db-user',
                'password': 'db-fixture-password',
                'host': 'db.example.test',
                'port': 5432,
                'dbname': 'forcad',
            },
            'rabbitmq': {
                'user': 'queue-user',
                'password': 'queue-fixture-password',
                'host': 'queue.example.test',
                'port': 5672,
                'vhost': 'forcad',
            },
            'redis': {
                'password': 'redis-fixture-password',
                'host': 'redis.example.test',
                'port': 6379,
                'db': 0,
            },
        }
        expected_storages = copy.deepcopy(config['storages'])

        with TemporaryDirectory() as directory, ExitStack() as stack:
            root = Path(directory)
            config_path = root / 'config.yml'
            config_path.write_text(yaml.safe_dump(config))
            for name, value in {
                'BASE_DIR': root,
                'CONFIG_PATH': config_path,
                'ADMIN_ENV_PATH': root / 'admin.env',
                'POSTGRES_ENV_PATH': root / 'postgres.env',
                'RABBITMQ_ENV_PATH': root / 'rabbitmq.env',
                'REDIS_ENV_PATH': root / 'redis.env',
            }.items():
                stack.enter_context(patch.object(constants, name, value))

            result = CliRunner().invoke(cli, ['setup'])
            self.assertEqual(result.exit_code, 0, result.output)
            saved = yaml.safe_load(config_path.read_text())
            self.assertEqual(saved['storages'], expected_storages)
            for storage_config in expected_storages.values():
                self.assertNotIn(storage_config['password'], result.output)
            private_files = [config_path, *root.glob('*.env'), *root.glob('config_backup*.yml')]
            for private_file in private_files:
                self.assertEqual(private_file.stat().st_mode & 0o777, 0o600)


class TeamTokenInitializationTestCase(TestCase):
    def setUp(self):
        self.cursor = MagicMock()
        self.cursor.fetchone.side_effect = [(1,), (2,), (1,), (2,)]

    def test_reinitialization_keeps_only_configured_token(self):
        config = game_config()['teams']
        original = copy.deepcopy(config)
        with patch.object(Team, 'generate_token', side_effect=[
            '1111111111111111', '2222222222222222',
        ]) as generate:
            first = init_teams(config, self.cursor)
            second = init_teams(config, self.cursor)
        self.assertEqual(first[0].token, FIXED_TOKEN)
        self.assertEqual(second[0].token, FIXED_TOKEN)
        self.assertEqual(first[1].token, '1111111111111111')
        self.assertEqual(second[1].token, '2222222222222222')
        self.assertEqual(generate.call_count, 2)
        self.assertEqual(config, original)
        stored_tokens = [call.args[1]['token']
                         for call in self.cursor.execute.call_args_list]
        self.assertEqual(stored_tokens, [team.token for team in first + second])
        for team, call in zip(first + second, self.cursor.execute.call_args_list):
            self.assertEqual(call.args[1]['logo_path'], team.logo_path)

    def test_team_logo_fallback_uses_name_and_unknown_is_empty(self):
        self.assertEqual(len(TEAM_LOGOS), 20)
        for name, logo_path in TEAM_LOGOS.items():
            with self.subTest(name=name):
                team = Team(
                    name=name,
                    ip='192.0.2.1',
                    token=FIXED_TOKEN,
                    highlighted=False,
                    active=True,
                    logo_path='',
                )
                self.assertEqual(team.logo_path, logo_path)

        unknown = Team(
            name='Unknown Team',
            ip='192.0.2.1',
            token=FIXED_TOKEN,
            highlighted=False,
            active=True,
            logo_path='',
        )
        self.assertEqual(unknown.logo_path, '')

    def test_missing_tokens_use_original_generator(self):
        config = game_config()['teams']
        config[0].pop('token')
        with patch.object(
            Team, 'generate_token', wraps=Team.generate_token,
        ) as generate:
            teams = init_teams(config, self.cursor)
        self.assertEqual(generate.call_count, len(config))
        for team in teams:
            self.assertRegex(team.token, r'^[0-9a-f]{16}$')

    def test_fixed_token_does_not_call_generator(self):
        with patch.object(Team, 'generate_token') as generate:
            team, = init_teams(game_config()['teams'][:1], self.cursor)
        generate.assert_not_called()
        self.assertEqual(team.token, FIXED_TOKEN)

    def test_null_token_generated(self):
        config = game_config()['teams'][:1]
        config[0]['token'] = None
        with patch.object(Team, 'generate_token', return_value=FIXED_TOKEN) as generate:
            teams = init_teams(config, self.cursor)
        generate.assert_called_once_with()
        self.assertEqual(teams[0].token, FIXED_TOKEN)

    def test_invalid_tokens_rejected_before_insert(self):
        for token in INVALID_TOKENS:
            with self.subTest(token=token):
                config = game_config()['teams']
                config[1]['token'] = token
                with self.assertRaises(ValueError):
                    init_teams(config, self.cursor)
                self.cursor.execute.assert_not_called()

    def test_duplicate_tokens_rejected_before_insert(self):
        config = game_config()['teams']
        config[1]['token'] = FIXED_TOKEN
        with self.assertRaises(ValueError):
            init_teams(config, self.cursor)
        self.cursor.execute.assert_not_called()

    def test_configured_token_stays_private(self):
        team, = init_teams(game_config()['teams'][:1], self.cursor)
        self.assertNotIn('token', team.to_dict_for_participants())


class TeamTokenMigrationTestCase(TestCase):
    def _cursor_for_migration(self, *, invalid_ids=None, duplicate_ids=None):
        connection = MagicMock()
        cursor = MagicMock()
        cursor.fetchone.side_effect = [(10,), None, None]
        cursor.fetchall.side_effect = [
            [('id',), ('token',)],
            invalid_ids or [],
            duplicate_ids or [],
        ]

        @contextmanager
        def db_cursor():
            yield connection, cursor

        return connection, cursor, db_cursor

    def test_duplicate_tokens_stop_before_schema_changes(self):
        _, cursor, db_cursor = self._cursor_for_migration(
            duplicate_ids=[([2, 7],)],
        )
        with patch.object(apply_fixes.utils, 'db_cursor', db_cursor):
            with self.assertRaisesRegex(ValueError, 'duplicate token rows'):
                apply_fixes.main()

        self.assertFalse(any(
            'ALTER TABLE' in call.args[0]
            for call in cursor.execute.call_args_list
        ))
        self.assertNotIn(FIXED_TOKEN, str(cursor.execute.call_args_list))

    def test_invalid_tokens_stop_before_schema_changes(self):
        _, cursor, db_cursor = self._cursor_for_migration(
            invalid_ids=[(3,)],
        )
        with patch.object(apply_fixes.utils, 'db_cursor', db_cursor):
            with self.assertRaisesRegex(ValueError, 'invalid token rows'):
                apply_fixes.main()

        self.assertFalse(any(
            'ALTER TABLE' in call.args[0]
            for call in cursor.execute.call_args_list
        ))

    def test_migration_adds_logo_and_token_constraints(self):
        connection, cursor, db_cursor = self._cursor_for_migration()
        with patch.object(apply_fixes.utils, 'db_cursor', db_cursor):
            apply_fixes.main()

        migration_sql = '\n'.join(
            call.args[0] for call in cursor.execute.call_args_list
        )
        self.assertIn('ADD COLUMN IF NOT EXISTS logo_path', migration_sql)
        self.assertIn('teams_token_format_check', migration_sql)
        self.assertIn('teams_token_unique', migration_sql)
        connection.commit.assert_called_once()


class TeamTokenUpdateRaceTestCase(TestCase):
    def setUp(self):
        import fakeredis

        self.redis = fakeredis.FakeRedis(decode_responses=True)
        self.redis_patch = patch.object(
            storage.utils.RedisStorage, 'get', return_value=self.redis
        )
        self.redis_patch.start()
        self.addCleanup(self.redis_patch.stop)
        self.credentials_patch = patch(
            'services.admin.viewsets.authentication.config.get_web_credentials',
            return_value=SimpleNamespace(username='organizer', password='test-pass'),
        )
        self.credentials_patch.start()
        self.addCleanup(self.credentials_patch.stop)

        self.app = Flask('team-token-race-test')
        self.app.register_blueprint(admin_bp, url_prefix='/api/admin/')
        self.client = self.app.test_client()
        self.client.post(
            '/api/admin/login/',
            json={'username': 'organizer', 'password': 'test-pass'},
        )

    def test_unique_constraint_race_returns_conflict(self):
        team = Team(
            id=1,
            name='Team',
            ip='192.0.2.1',
            token=FIXED_TOKEN,
            highlighted=False,
            active=True,
            logo_path='',
        )
        response_payload = {
            'name': 'Team',
            'ip': '192.0.2.1',
            'token': 'fedcba98' + '76543210',
            'highlighted': False,
            'active': True,
            'logo_path': '',
        }
        with patch.object(
            storage.teams, 'get_all_teams', return_value=[team]
        ), patch.object(
            storage.teams,
            'update_team',
            side_effect=UniqueViolation('duplicate team token'),
        ), patch.object(events, 'refresh_scoreboard_after_commit'):
            response = self.client.put(
                '/api/admin/teams/1/',
                json=response_payload,
            )

        self.assertEqual(response.status_code, 409)

import copy
import sys
from contextlib import ExitStack
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import TestCase
from unittest.mock import MagicMock, patch

import yaml
from click.testing import CliRunner
from pydantic import ValidationError

PROJECT_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_DIR))
sys.path.insert(0, str(PROJECT_DIR / 'backend'))

from cli import cli, constants
from cli.models import BasicConfig
from lib.models import Team
from scripts.init_db import init_teams


FIXED_TOKEN = '0123456789abcdef'
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

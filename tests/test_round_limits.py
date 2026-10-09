"""Round limit validation and configuration loading contracts."""

import sys
from copy import deepcopy
from datetime import datetime
from pathlib import Path
from unittest import TestCase
from unittest.mock import MagicMock

import yaml
from pydantic import ValidationError

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(ROOT / 'backend'))

from cli.models import GameConfig as CliGameConfig
from lib.models.game_config import GameConfig
from scripts.init_db import init_game_config


GAME = dict(
    flag_lifetime=5, round_time=60, rounds=300,
    start_time=datetime(2026, 10, 10, 10), timezone='Europe/Moscow',
    game_hardness=10.0, inflation=True, volga_attacks_mode=False,
    mode='classic', real_round=0, game_running=False,
)


class RoundLimitTests(TestCase):
    def test_setup_and_initializer_preserve_round_limit_and_start(self):
        configured = CliGameConfig.model_validate(GAME, strict=True)
        restored = yaml.safe_load(yaml.safe_dump(configured.model_dump()))
        cursor = MagicMock()
        cursor.fetchone.return_value = (1,)
        init_game_config(restored, cursor)
        saved = cursor.execute.call_args.args[1]
        self.assertEqual(saved['rounds'], 300)
        self.assertEqual(saved['start_time'], '2026-10-10 10:00:00+03:00')
        self.assertEqual(saved['real_round'], 0)
        self.assertFalse(saved['game_running'])

    def test_omitted_or_null_limit_preserves_unlimited_games_and_old_cache(self):
        legacy = {k: v for k, v in GAME.items() if k != 'rounds'}
        for config in (legacy, {**legacy, 'rounds': None}):
            with self.subTest(config=config):
                self.assertIsNone(CliGameConfig.model_validate(config).rounds)
                model = GameConfig(**config)
                self.assertIsNone(model.rounds)
                self.assertIsNone(GameConfig.from_json(model.to_json()).rounds)

    def test_invalid_limit_is_rejected_by_cli_and_direct_initializer(self):
        for rounds in (0, -1, 1.5, 300.0, True, '300', 2147483648):
            with self.subTest(rounds=rounds):
                config = {**GAME, 'rounds': rounds}
                with self.assertRaises(ValidationError):
                    CliGameConfig.model_validate(config)
                cursor = MagicMock()
                with self.assertRaises(ValueError):
                    init_game_config(deepcopy(config), cursor)
                cursor.execute.assert_not_called()

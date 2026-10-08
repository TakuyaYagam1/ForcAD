"""Organizer controls require a session and enforce terminal game state."""

import sys
from pathlib import Path
from datetime import datetime, timezone
from types import SimpleNamespace
from unittest import TestCase
from unittest.mock import MagicMock, patch

import fakeredis
from flask import Flask

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'backend'))

from lib import config, storage
from lib.helpers import events
from services.admin.viewsets import admin_bp
from services.ticker.__main__ import main
from services.ticker.models import Schedule, TickerState


class GameControlTests(TestCase):
    def setUp(self):
        self.redis = fakeredis.FakeRedis(decode_responses=True)
        for target, name, value in (
            (storage.utils.RedisStorage, 'get', self.redis),
            (config, 'get_web_credentials', SimpleNamespace(username='organizer', password='test-password')),
        ):
            mocked = patch.object(target, name, return_value=value)
            mocked.start()
            self.addCleanup(mocked.stop)
        self.app = Flask('game-control-test')
        self.app.register_blueprint(admin_bp, url_prefix='/api/admin')
        self.client = self.app.test_client()

    def login(self):
        response = self.client.post('/api/admin/login/', json={
            'username': 'organizer', 'password': 'test-password',
        })
        self.assertEqual(response.status_code, 200)

    def test_control_rejects_anonymous_requests_and_cross_origin(self):
        with patch.object(storage.game, 'change_game_state') as change:
            for action in ('pause', 'resume', 'finish'):
                self.assertEqual(self.client.post(f'/api/admin/game/{action}/', json={'confirm': True}).status_code, 403)
            self.login()
            self.assertEqual(self.client.post('/api/admin/game/finish/', json={'confirm': True},
                                             headers={'Origin': 'https://other.example'}).status_code, 403)
            self.assertEqual(self.client.get('/api/admin/game/finish/').status_code, 405)
            change.assert_not_called()

    def test_finish_requires_explicit_boolean_confirmation(self):
        self.login()
        with patch.object(storage.game, 'change_game_state') as change:
            for body in ({}, {'confirm': False}, {'confirm': 'true'}, {'confirm': 1}, []):
                self.assertEqual(self.client.post('/api/admin/game/finish/', json=body).status_code, 400)
            change.assert_not_called()

    def test_actions_return_authoritative_state(self):
        self.login()
        for action, phase in (('pause', 'paused'), ('resume', 'running'), ('finish', 'finished')):
            with self.subTest(action=action), patch.object(storage.game, 'change_game_state') as change, \
                    patch.object(storage.game, 'get_runtime_status', return_value={'phase': phase}), \
                    patch.object(events, 'refresh_scoreboard_after_commit') as refresh:
                response = self.client.post(f'/api/admin/game/{action}/', json={'confirm': True})
                self.assertEqual(response.status_code, 200)
                self.assertEqual(response.json['phase'], phase)
                change.assert_called_once_with(action)
                refresh.assert_called_once_with()

    def test_finished_game_rejects_resume_as_conflict(self):
        self.login()
        with patch.object(storage.game, 'change_game_state', side_effect=ValueError('Game has already finished')):
            self.assertEqual(self.client.post('/api/admin/game/resume/', json={}).status_code, 409)

    def test_unknown_action_does_not_change_game(self):
        self.login()
        with patch.object(storage.game, 'change_game_state') as change:
            self.assertEqual(self.client.post('/api/admin/game/restart/', json={}).status_code, 404)
            change.assert_not_called()

    def test_finished_status_survives_missing_redis_state(self):
        with patch.object(storage.game, 'get_lifecycle', return_value=(14, False)), \
                patch.object(storage.game, 'get_current_game_config', return_value=SimpleNamespace(round_time=60)), \
                patch.object(storage.game, 'has_pending_checks', return_value=True):
            status = storage.game.get_runtime_status()
        self.assertEqual(status['phase'], 'finished')
        self.assertEqual(status['round'], 14)
        self.assertTrue(status['results_pending'])

    def test_final_snapshot_waits_for_results_and_publishes_once(self):
        with patch.object(storage.game, 'is_game_finished', return_value=True), \
                patch.object(storage.game, 'has_pending_checks', return_value=True) as pending, \
                patch.object(storage.game, 'get_real_round_from_db', return_value=14), \
                patch.object(storage.game, 'update_game_state') as publish:
            storage.game.finalize_finished_game()
            publish.assert_not_called()
            pending.return_value = False
            storage.game.finalize_finished_game()
            storage.game.finalize_finished_game()
            publish.assert_called_once_with(14)

    def test_restarted_ticker_does_not_restart_a_finished_game(self):
        callback = MagicMock()
        state = TickerState(MagicMock(), False, [
            Schedule('start_game', datetime.fromtimestamp(0, timezone.utc), callback),
        ])
        with patch.object(storage.game, 'is_game_finished', return_value=True), \
                patch.object(storage.game, 'finalize_finished_game') as finalize, \
                patch('services.ticker.__main__.sync_blitz_schedules'), \
                patch('services.ticker.__main__.recover_expired_jobs'), \
                patch.object(events, 'retry_scoreboard_refresh'), \
                patch('services.ticker.__main__.time.sleep', side_effect=InterruptedError):
            with self.assertRaises(InterruptedError):
                main(state)
        callback.assert_not_called()
        finalize.assert_called_once_with()

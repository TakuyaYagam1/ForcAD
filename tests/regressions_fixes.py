import os

os.environ.update(ADMIN_USERNAME='organizer', ADMIN_PASSWORD='test-password')
import unittest
from contextlib import contextmanager
from types import SimpleNamespace
from unittest.mock import patch, MagicMock
from flask import Flask
from psycopg2.errors import UniqueViolation
import fakeredis
from lib import models, storage
from lib.helpers import events
from services.admin.viewsets import admin_bp
from services.api.views import client_bp
from services.tasks.actions import put_action
from services.ticker.hooks import blitz_tasks

TEAM = dict(
    name='Test team',
    ip='10.1.2.3',
    token='abcdef0123456789',
    highlighted=False,
    active=True,
    logo_path='',
)
TASK = dict(
    name='Vault',
    checker='/checkers/vault/checker.py',
    gets=1,
    puts=1,
    places=1,
    checker_timeout=10,
    checker_type='hackerdom',
    env_path='',
    get_period=10,
    default_score=2500,
    active=True,
)
CFG = dict(
    flag_lifetime=5,
    game_hardness=10,
    inflation=True,
    volga_attacks_mode=False,
    round_time=60,
    mode='classic',
    timezone='UTC',
    start_time='2026-10-01T00:00:00+00:00',
    real_round=3,
    game_running=True,
)


class RegressionTests(unittest.TestCase):
    def setUp(self):
        self.redis = fakeredis.FakeRedis(decode_responses=True)
        self.redis_patch = patch.object(
            storage.utils.RedisStorage, 'get', return_value=self.redis
        )
        self.redis_patch.start()
        self.addCleanup(self.redis_patch.stop)
        self.app = Flask('fixes-test')
        self.app.testing = True
        self.app.register_blueprint(admin_bp, url_prefix='/api/admin')
        self.app.register_blueprint(client_bp, url_prefix='/api/client')
        self.client = self.app.test_client()

    def login(self):
        return self.client.post(
            '/api/admin/login/',
            json={'username': 'organizer', 'password': 'test-password'},
        )

    def test_task_update_accepts_empty_checker_tags(self):
        self.login()
        data = {**TASK, 'checker_type': ''}
        task = models.Task(id=1, **data)
        with patch.object(storage.tasks, 'get_all_tasks', return_value=[task]), \
                patch.object(storage.tasks, 'update_task', return_value=task) as update, \
                patch.object(events, 'refresh_scoreboard_after_commit'):
            response = self.client.put('/api/admin/tasks/1/', json=data)
        self.assertEqual(response.status_code, 200, response.get_data(as_text=True))
        self.assertEqual(response.json['checker_type'], '')
        self.assertEqual(update.call_args.args[0].checker_type, '')

    def test_login_status_logout_invalidates_cookie_and_session(self):
        self.assertEqual(self.client.get('/api/admin/login/').status_code, 405)
        self.assertEqual(self.client.get('/api/admin/status/').status_code, 403)
        self.assertEqual(
            self.client.post('/api/admin/login/', json=[]).status_code, 400
        )
        self.assertEqual(self.login().json['username'], 'organizer')
        self.assertEqual(self.client.get('/api/admin/status/').status_code, 200)
        cookie = self.client.get_cookie('session').value
        self.assertTrue(self.redis.exists(storage.keys.CacheKeys.session(cookie)))
        self.assertEqual(self.client.post('/api/admin/logout/').status_code, 200)
        self.assertFalse(self.redis.exists(storage.keys.CacheKeys.session(cookie)))
        self.client.set_cookie('session', cookie)
        self.assertEqual(self.client.get('/api/admin/status/').status_code, 403)

    def test_crud_validation_404_generated_token_and_inactive_list(self):
        self.login()
        active = models.Team(id=1, **TEAM)
        inactive = models.Team(
            id=2, **{**TEAM, 'active': False, 'token': 'second-token'}
        )
        with patch.object(
            storage.teams, 'get_all_teams', return_value=[active, inactive]
        ), patch.object(
            storage.teams,
            'create_team',
            side_effect=lambda team: models.Team(**{**team.to_dict(), "id": 3}),
        ), patch.object(
            events, 'init_scoreboard'
        ) as refresh:
            self.assertEqual(len(self.client.get('/api/admin/teams/').json), 2)
            self.assertEqual(
                self.client.put('/api/admin/teams/99/', json=TEAM).status_code, 404
            )
            self.assertEqual(
                self.client.delete('/api/admin/teams/99/').status_code, 404
            )
            for invalid in (
                None,
                [],
                {},
                {**TEAM, 'name': ' '},
                {**TEAM, 'ip': '999.1.2.3'},
                {**TEAM, 'active': 'false'},
            ):
                self.assertEqual(
                    self.client.post('/api/admin/teams/', json=invalid).status_code, 400
                )
            response = self.client.post(
                '/api/admin/teams/', json={**TEAM, 'token': 'USER-SUPPLIED'}
            )
            self.assertEqual(response.status_code, 201)
            self.assertEqual(len(response.json['token']), 16)
            self.assertNotEqual(response.json['token'], 'USER-SUPPLIED')
            refresh.assert_called_once_with(refresh_state=True)
        with patch.object(storage.tasks, 'get_all_tasks', return_value=[]):
            self.assertEqual(
                self.client.put('/api/admin/tasks/99/', json=TASK).status_code, 404
            )
        for field, value in (
            ('places', 0),
            ('gets', -1),
            ('puts', 1.5),
            ('checker_timeout', 0),
            ('get_period', 0),
            ('default_score', -1),
            ('checker', ''),
            ('checker_type', 'a' * 33),
        ):
            self.assertEqual(
                self.client.post(
                    '/api/admin/tasks/', json={**TASK, field: value}
                ).status_code,
                400,
                (field, value),
            )

    def test_db_token_lookup_ignores_stale_redis_mapping(self):
        self.redis.set(storage.keys.CacheKeys.team_by_token('revoked'), 123)
        cursor = MagicMock()
        cursor.fetchone.return_value = None

        @contextmanager
        def db(*args, **kwargs):
            yield MagicMock(), cursor

        with patch.object(storage.utils, 'db_cursor', db):
            self.assertIsNone(storage.teams.get_team_id_by_token('revoked'))
            self.assertIn('active=TRUE', cursor.execute.call_args.args[0])
            cursor.fetchone.return_value = (7,)
            self.assertEqual(storage.teams.get_team_id_by_token('new-token'), 7)

    def test_evicted_stolen_flag_cache_is_restored(self):
        cursor = MagicMock()
        cursor.fetchall.return_value = [(42,)]

        @contextmanager
        def db(*args, **kwargs):
            yield MagicMock(), cursor

        with patch.object(storage.utils, 'db_cursor', db), patch.object(
            storage.game,
            'get_current_game_config',
            return_value=SimpleNamespace(flag_lifetime=5),
        ):
            self.assertFalse(
                storage.flags.try_add_stolen_flag(SimpleNamespace(id=42), 1, 10)
            )
            self.assertTrue(
                storage.flags.try_add_stolen_flag(SimpleNamespace(id=43), 1, 10)
            )
            self.assertFalse(
                storage.flags.try_add_stolen_flag(SimpleNamespace(id=43), 1, 10)
            )

    def test_duplicate_db_submit_returns_result_and_rolls_back(self):
        flag = SimpleNamespace(team_id=2, task_id=3, round=10, id=42)
        cursor = MagicMock()
        cursor.callproc.side_effect = UniqueViolation('duplicate flag')
        conn = MagicMock()
        conn.cursor.return_value = cursor
        pool = MagicMock()
        pool.getconn.return_value = conn
        with patch.object(
            storage.flags, 'get_flag_by_str', return_value=flag
        ), patch.object(
            storage.flags, 'try_add_stolen_flag', return_value=True
        ), patch.object(
            storage.game,
            'get_current_game_config',
            return_value=SimpleNamespace(flag_lifetime=5, volga_attacks_mode=False),
        ), patch.object(
            storage.utils.DBPool, 'get', return_value=pool
        ):
            result = storage.attacks.handle_attack(1, 'FLAG', 10)
            self.assertFalse(result.submit_ok)
            self.assertIn('already stolen', result.message)
            conn.rollback.assert_called_once()

    def test_ctftime_zero_checks_do_not_divide_by_zero(self):
        state = models.GameState(
            round=2,
            round_start=1,
            team_tasks=[
                dict(team_id=1, score=200, checks_passed=0, checks=0),
                dict(team_id=1, score=300, checks_passed=1, checks=2),
            ],
        )
        with patch.object(
            storage.game, 'get_cached_game_state', return_value=state
        ), patch.object(
            storage.teams, 'get_teams', return_value=[models.Team(id=1, **TEAM)]
        ):
            self.assertEqual(
                storage.game.construct_ctftime_scoreboard()[0]['score'], 150
            )

    def test_hardness_one_nan_and_infinity_rejected(self):
        for hardness in (1, 0, float('nan'), float('inf')):
            with self.assertRaises(ValueError):
                models.GameConfig(**{**CFG, 'game_hardness': hardness})
        self.assertEqual(models.GameConfig(**CFG).game_hardness, 10)

    def test_failed_check_skips_put_without_flag_creation(self):
        verdict = models.CheckerVerdict(
            action=models.Action.CHECK,
            status=models.TaskStatus.DOWN,
            command='',
            public_message='down',
            private_message='',
        )
        with patch.object(models.Flag, 'generate') as generate:
            result = put_action.run(
                verdict, models.Team(id=1, **TEAM), models.Task(id=1, **TASK), 10
            )
            self.assertEqual(result.action, models.Action.PUT)
            self.assertEqual(result.status, models.TaskStatus.DOWN)
            generate.assert_not_called()

    def test_blitz_get_runner_submits_check_gets_not_puts(self):
        with patch.object(
            storage.game, 'get_real_round', return_value=10
        ), patch.object(
            blitz_tasks.utils,
            'get_round_processor_args',
            return_value=[('team', 'task', 10)],
        ), patch.object(
            blitz_tasks, 'submit_puts_jobs'
        ) as puts, patch.object(
            blitz_tasks, 'submit_check_gets_jobs'
        ) as gets:
            blitz_tasks.blitz_check_gets_runner_factory(7)(
                SimpleNamespace(celery_app='app')
            )
            gets.assert_called_once_with('app', 'team', 'task', 10)
            puts.assert_not_called()

    def test_pause_resume_idempotent_and_reported_over_http(self):
        self.redis.set(storage.keys.CacheKeys.current_round(), 3)
        with patch.object(
            storage.game,
            'get_current_game_config',
            return_value=SimpleNamespace(round_time=60),
        ), patch(
            'lib.storage.game.time',
            SimpleNamespace(time=MagicMock(side_effect=[100, 105, 120, 140])),
        ):
            storage.game.set_game_paused(True)
            storage.game.set_game_paused(True)
            self.assertEqual(
                self.client.get('/api/client/status/').json['paused_at'], 100
            )
            storage.game.set_game_paused(False)
            storage.game.set_game_paused(False)
            data = self.client.get('/api/client/status/').json
            self.assertEqual(data['phase'], 'running')
            self.assertEqual(data['paused_seconds'], 20)

    def test_crud_refresh_keeps_completed_cells_adds_new_cells(self):
        old = models.GameState(
            round=2, round_start=100, team_tasks=[dict(team_id=1, task_id=1, score=100)]
        )
        fresh = models.GameState(
            round=2,
            round_start=100,
            team_tasks=[
                dict(team_id=1, task_id=1, score=999),
                dict(team_id=2, task_id=1, score=2500),
            ],
        )
        self.redis.set(storage.keys.CacheKeys.game_state(), old.to_json())
        with patch.object(
            storage.game, 'construct_game_state_from_db', return_value=fresh
        ), patch.object(
            storage.teams, 'get_teams', return_value=[models.Team(id=1, **TEAM)]
        ), patch.object(
            storage.tasks, 'get_tasks', return_value=[models.Task(id=1, **TASK)]
        ), patch.object(
            storage.game,
            'get_current_game_config',
            return_value=models.GameConfig(**CFG),
        ):
            data = storage.game.construct_scoreboard(refresh_state=True)
            self.assertEqual(
                [cell['score'] for cell in data['state']['team_tasks']], [100, 2500]
            )
            self.assertTrue(self.redis.get(storage.keys.CacheKeys.game_state()))

    def test_refresh_retries_if_a_round_changes_during_db_read(self):
        old = models.GameState(
            round=2, round_start=100, team_tasks=[dict(team_id=1, task_id=1, score=100)]
        )
        new = models.GameState(
            round=3, round_start=160, team_tasks=[dict(team_id=1, task_id=1, score=333)]
        )
        self.redis.set(storage.keys.CacheKeys.game_state(), old.to_json())
        count = 0

        def fresh(round):
            nonlocal count
            count += 1
            if count == 1:
                self.redis.set(storage.keys.CacheKeys.game_state(), new.to_json())
            return models.GameState(
                round=round,
                round_start=100,
                team_tasks=[
                    dict(team_id=1, task_id=1, score=999),
                    dict(team_id=2, task_id=1, score=50),
                ],
            )

        with patch.object(
            storage.game, 'construct_game_state_from_db', side_effect=fresh
        ):
            data = storage.game.refresh_cached_game_state()
            self.assertEqual(data.round, 3)
            self.assertEqual(data.team_tasks[0]['score'], 333)
            self.assertEqual(count, 2)


if __name__ == '__main__':
    unittest.main(verbosity=2)

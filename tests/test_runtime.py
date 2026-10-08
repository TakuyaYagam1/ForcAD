"""Offline contracts for checker accounting, caching and queue serialization."""

import json
import sys
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace
from unittest import TestCase
from unittest.mock import MagicMock, patch

import fakeredis
from celery import Celery, current_app
from celery.exceptions import Ignore
from kombu.serialization import dumps, loads

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'backend'))

from lib import models, storage
from lib.helpers import events
from lib.storage import dispatch
from lib.storage.keys import CacheKeys
from lib.team_logos import TEAM_LOGOS
from services.tasks.handlers import checker_results_handler
from services.tasks import actions
from services.tasks import celery_factory
from services.ticker.hooks import blitz_tasks, classic_round, utils as hooks
from scripts import reset_db


def team(team_id=1):
    return models.Team(
        id=team_id, name=f'Team {team_id}', ip=f'10.0.0.{team_id}',
        token=f'{team_id:016x}',
    )


def task(puts=1):
    return models.Task(
        id=1, name='Service', checker='/checkers/service', gets=1, puts=puts,
        places=1, checker_timeout=10, checker_type='', env_path='',
        get_period=10, default_score=2500,
    )


def verdict(action=models.Action.CHECK, status=models.TaskStatus.UP):
    return models.CheckerVerdict(
        action=action, status=status, command='',
        public_message='OK', private_message='',
    )


class RuntimeTests(TestCase):
    def setUp(self):
        self.redis = fakeredis.FakeRedis(decode_responses=True)
        redis_patch = patch.object(
            storage.utils.RedisStorage, 'get', return_value=self.redis,
        )
        redis_patch.start()
        self.addCleanup(redis_patch.stop)

    def test_temporary_celery_queues_are_exclusive_for_rabbitmq(self):
        from lib.config.models import Celery as CeleryConfig

        previous_app = current_app._get_current_object()
        self.addCleanup(previous_app.set_current)
        config = CeleryConfig(
            broker_url='memory://', result_backend='cache+memory://', timezone='UTC',
        )
        with patch.object(celery_factory.config, 'get_celery_config', return_value=config):
            app = celery_factory.get_celery_app()
        self.addCleanup(app.close)
        control_queue = app.control.mailbox.get_queue('fixture-worker')
        receiver = app.events.Receiver(app.connection_for_read())
        self.assertTrue(control_queue.exclusive)
        self.assertTrue(receiver.queue.exclusive)
        self.assertFalse(control_queue.durable)
        self.assertFalse(receiver.queue.durable)
        self.assertTrue(app.amqp.queues['celery'].durable)

    def test_active_flag_is_loaded_from_database_after_cache_miss(self):
        flag = models.Flag(
            id=7, team_id=2, task_id=1, round=3, flag='S' * 31 + '=',
            public_flag_data='public', private_flag_data='private', vuln_number=1,
        )
        self.redis.set(CacheKeys.flags_cached(), 1)
        cursor = MagicMock()
        cursor.fetchone.return_value = flag.to_dict()

        @contextmanager
        def database(**_):
            yield MagicMock(), cursor

        with patch.object(storage.utils, 'db_cursor', database), patch.object(
            storage.game, 'get_current_game_config',
            return_value=SimpleNamespace(flag_lifetime=5, round_time=60),
        ):
            restored = storage.flags.get_flag_by_str(flag.flag, 4)
            self.assertEqual(restored.to_dict(), flag.to_dict())
            second = storage.flags.get_flag_by_id(flag.id, 4)
        self.assertEqual(second.id, flag.id)
        self.assertEqual(cursor.execute.call_count, 1)
        self.assertGreater(self.redis.ttl(CacheKeys.flag_by_str(flag.flag)), 0)

    def test_worker_models_roundtrip_using_only_json(self):
        original = [team(), task(), verdict()]
        content_type, encoding, payload = dumps(original, serializer='json')
        restored = loads(payload, content_type, encoding, accept={'application/json'})
        for before, after in zip(original, restored):
            self.assertIs(type(after), type(before))
            self.assertEqual(after.to_dict(), before.to_dict())

    def test_reset_clears_only_the_selected_redis_database(self):
        server = fakeredis.FakeServer()
        selected = fakeredis.FakeRedis(server=server, db=0)
        unrelated = fakeredis.FakeRedis(server=server, db=1)
        selected.set('game', 'fixture')
        unrelated.set('keep', 'other application')
        settings = MagicMock()
        settings.model_dump.return_value = {}
        with patch.object(reset_db.config, 'get_db_config', return_value=settings), \
                patch.object(reset_db.config, 'get_redis_config', return_value=settings), \
                patch.object(reset_db.psycopg2, 'connect'), \
                patch.object(reset_db.redis, 'Redis', return_value=selected):
            reset_db.run()
        self.assertFalse(selected.exists('game'))
        self.assertEqual(unrelated.get('keep'), b'other application')

    def test_socketio_publisher_uses_json_protocol(self):
        manager = storage.utils.ReliableKombuManager(
            'amqp://guest:guest@localhost//', write_only=True,
        )
        publisher = MagicMock()
        with patch.object(manager, '_producer_publish', return_value=publisher):
            manager._publish({'method': 'emit', 'data': {'round': 3}})
        self.assertEqual(
            json.loads(publisher.call_args.args[0]),
            {'method': 'emit', 'data': {'round': 3}},
        )

    def test_replayed_snapshot_is_recorded_once(self):
        state = models.GameState(
            round=3, round_start=100,
            team_tasks=[dict(team_id=1, task_id=1, score=2500)],
        )
        with patch.object(storage.utils.SIOManager, 'write_only'):
            storage.game.update_game_state(3, state)
            storage.game.update_game_state(3, state)
        self.assertEqual(self.redis.xlen(CacheKeys.teamtasks_history(1, 1)), 1)

    def test_empty_blitz_puts_do_not_produce_a_failed_check(self):
        app = MagicMock()
        blitz_tasks.submit_puts_jobs(app, team(), task(puts=0), 3)
        app.signature.assert_not_called()

    def test_nested_celery_results_preserve_check_failure_priority(self):
        check = verdict(status=models.TaskStatus.DOWN)
        put = verdict(models.Action.PUT, models.TaskStatus.MUMBLE)
        with patch.object(storage.tasks, 'update_task_status') as update:
            result = checker_results_handler.run(
                [[put], check, verdict(models.Action.GET)], team(), task(), 3,
                job_id='fixture-job',
            )
        self.assertEqual(result.status, models.TaskStatus.DOWN)
        self.assertEqual(update.call_args.kwargs['job_id'], 'fixture-job')

    def test_closed_checker_jobs_do_not_contact_services(self):
        calls = (
            (actions.check_action, (team(), task(), 1)),
            (actions.put_action, (verdict(), team(), task(), 1)),
            (actions.get_action, (verdict(), team(), task(), 1)),
        )
        for action, args in calls:
            with self.subTest(action=action.name), patch.object(
                dispatch, 'touch_job', return_value=False,
            ), patch.object(actions.checkers, 'CheckerRunner') as runner:
                with self.assertRaises(Ignore):
                    action.run(*args, job_id='closed-job')
                runner.assert_not_called()

    def test_metadata_reconciles_without_a_surviving_redis_marker(self):
        with patch.object(events, '_last_reconcile', 0), patch.object(
            events.time, 'monotonic', return_value=100,
        ), patch.object(events, 'init_scoreboard') as refresh, patch.object(
            storage.caching, 'flush_teams_cache',
        ) as teams, patch.object(storage.caching, 'flush_tasks_cache') as tasks:
            events.retry_scoreboard_refresh()
        refresh.assert_called_once_with(refresh_state=True)
        teams.assert_called_once()
        tasks.assert_called_once()

    def test_pause_between_published_jobs_preserves_remaining_dispatch(self):
        jobs = [dict(id='first'), dict(id='second')]
        run = dict(id='batch')
        state = SimpleNamespace(celery_app='app', scheduled_at=None)
        with patch.object(storage.game, 'is_game_paused', side_effect=[False, False, True]), \
                patch.object(storage.teams, 'get_teams', return_value=[team()]), \
                patch.object(storage.tasks, 'get_tasks', return_value=[task()]), \
                patch.object(dispatch, 'prepare', return_value=run), \
                patch.object(dispatch, 'pending_jobs', return_value=jobs), \
                patch.object(dispatch, 'unpack_job', return_value=(team(), task(), 3)), \
                patch.object(dispatch, 'mark_sent') as sent, \
                patch.object(dispatch, 'finish') as finished:
            publish = MagicMock()
            completed = hooks.dispatch_jobs(state, 'periodic', publish)
        self.assertFalse(completed)
        self.assertEqual(publish.call_count, 1)
        sent.assert_called_once_with('first')
        finished.assert_not_called()

    def test_all_twenty_team_logos_exist_and_custom_logos_are_preserved(self):
        directory = Path(__file__).resolve().parents[1] / 'teams_logo'
        self.assertEqual(len(TEAM_LOGOS), 20)
        self.assertEqual(len(set(TEAM_LOGOS.values())), 20)
        for name, path in TEAM_LOGOS.items():
            with self.subTest(name=name):
                item = team()
                item.name = name
                restored = models.Team(**{**item.to_dict(), 'logo_path': None})
                self.assertEqual(restored.logo_path, path)
                self.assertTrue((directory / Path(path).name).is_file())
        custom = models.Team(**{**team().to_dict(), 'logo_path': '/custom.png'})
        self.assertEqual(custom.logo_path, '/custom.png')

    def test_real_celery_canvases_handle_zero_one_and_multiple_actions(self):
        previous_app = current_app._get_current_object()
        app = Celery('canvas_tests', broker='memory://', backend='cache+memory://')
        app.conf.update(task_always_eager=True, task_eager_propagates=True)
        app.set_current()
        self.assertIn(actions.check_action.name, app.tasks)
        self.addCleanup(app.close)
        self.addCleanup(previous_app.set_current)
        runner = MagicMock()
        runner.check.return_value = verdict()
        runner.put.return_value = verdict(models.Action.PUT)
        runner.get.return_value = verdict(models.Action.GET)

        for puts in (0, 1, 2):
            for gets in (0, 1, 2):
                service = task(puts)
                service.gets = gets
                with self.subTest(puts=puts, gets=gets), patch(
                    'lib.helpers.checkers.CheckerRunner', return_value=runner,
                ), patch.object(storage.flags, 'add_flag'), patch.object(
                    storage.flags, 'get_random_round_flag', return_value=MagicMock(),
                ), patch.object(
                    storage.game, 'get_current_game_config',
                    return_value=SimpleNamespace(flag_lifetime=5),
                ), patch.object(
                    dispatch, 'touch_job', return_value=True,
                ), patch.object(storage.tasks, 'update_task_status') as update:
                    classic_round.submit_full_round_jobs(app, team(), service, 1, 'classic')
                    self.assertEqual(update.call_count, 1)
                    self.assertEqual(update.call_args.kwargs['checker_verdict'].status, models.TaskStatus.UP)
                    update.reset_mock()
                    blitz_tasks.submit_check_gets_jobs(app, team(), service, 1, 'blitz-get')
                    self.assertEqual(update.call_count, 1)
                    update.reset_mock()
                    blitz_tasks.submit_puts_jobs(app, team(), service, 1, 'blitz-put')
                    self.assertEqual(update.call_count, int(puts > 0))

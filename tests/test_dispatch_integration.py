"""Accounting checks on an explicitly selected disposable local database."""

import os
import sys
from datetime import datetime, timezone
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from math import sqrt
from types import SimpleNamespace
from unittest import TestCase, skipUnless
from unittest.mock import patch

import fakeredis
import psycopg2
from psycopg2 import extensions, sql

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'backend'))

from lib import models, storage
from lib.storage import dispatch, schedules
from lib.storage.keys import CacheKeys
from scripts.apply_fixes import main as migrate
from services.ticker.hooks.utils import recover_expired_jobs
from test_runtime import team, task, verdict

DSN = os.getenv('FORCAD_TEST_DSN')
SCRIPTS = Path(__file__).resolve().parents[1] / 'backend/scripts'


@skipUnless(DSN, 'Requires disposable PostgreSQL via FORCAD_TEST_DSN')
class DispatchIntegrationTests(TestCase):
    @classmethod
    def setUpClass(cls):
        params = extensions.parse_dsn(DSN)
        if params.get('dbname') != 'forcad_test_dispatch':
            raise ValueError('Only forcad_test_dispatch is allowed')
        if not params.get('host', '').startswith('/'):
            raise ValueError('These tests require a local Unix socket')
        cls.schema = f'dispatch_tests_{os.getpid()}'
        cls.admin = psycopg2.connect(DSN)
        cls.admin.autocommit = True
        with cls.admin.cursor() as cursor:
            cursor.execute(sql.SQL('CREATE SCHEMA {}').format(sql.Identifier(cls.schema)))
        cls.pool = storage.utils.ConnectionPool(
            1, 2, dsn=DSN, options=f'-c search_path={cls.schema}',
        )

    @classmethod
    def tearDownClass(cls):
        cls.pool.closeall()
        with cls.admin.cursor() as cursor:
            cursor.execute(
                sql.SQL('DROP SCHEMA {} CASCADE').format(sql.Identifier(cls.schema)),
            )
        cls.admin.close()

    def setUp(self):
        connection = self.pool.getconn()
        try:
            with connection, connection.cursor() as cursor:
                cursor.execute((SCRIPTS / 'drop_query.sql').read_text())
                for name in ('create_tables.sql', 'create_functions.sql', 'create_dispatch.sql'):
                    cursor.execute((SCRIPTS / name).read_text())
                cursor.execute(
                    'INSERT INTO GameConfig '
                    '(id,real_round,game_hardness,flag_lifetime,round_time,inflation,game_running) '
                    'VALUES (1,0,10,5,60,TRUE,TRUE)',
                )
                for item in (team(1), team(2)):
                    cursor.execute(
                        'INSERT INTO Teams(id,name,ip,token,logo_path) VALUES (%s,%s,%s,%s,%s)',
                        (item.id, item.name, item.ip, item.token, item.logo_path),
                    )
                cursor.execute(
                    'INSERT INTO Tasks '
                    '(id,name,checker,gets,puts,places,checker_timeout,default_score) '
                    "VALUES (1,'Service','/checkers/service',1,1,1,10,2500)",
                )
                cursor.execute('SELECT fix_teamtasks()')
        finally:
            self.pool.putconn(connection)
        db_patch = patch.object(storage.utils.DBPool, 'get', return_value=self.pool)
        db_patch.start()
        self.addCleanup(db_patch.stop)
        self.fake_redis = fakeredis.FakeRedis(decode_responses=True)
        redis_patch = patch.object(
            storage.utils.RedisStorage, 'get',
            return_value=self.fake_redis,
        )
        redis_patch.start()
        self.addCleanup(redis_patch.stop)

    def plan_round(self):
        return dispatch.prepare(
            'classic_rounds', [team(1), team(2)], [task()], advances_round=True,
        )

    def test_controls_pause_without_advancing_and_resume_idempotently(self):
        self.plan_round()
        storage.game.change_game_state('pause')
        paused_at = self.fake_redis.get(storage.game.PAUSED_AT)
        storage.game.change_game_state('pause')
        self.assertEqual(self.fake_redis.get(storage.game.PAUSED_AT), paused_at)
        self.assertIsNone(self.plan_round())
        self.assertEqual(storage.game.get_real_round_from_db(), 1)
        storage.game.change_game_state('resume')
        storage.game.change_game_state('resume')
        self.assertFalse(storage.game.is_game_paused())
        self.assertEqual(self.plan_round()['round'], 1)

    def test_finish_drains_sent_jobs_cancels_unpublished_and_cannot_restart(self):
        run = self.plan_round()
        sent, unpublished = dispatch.pending_jobs(run['id'])
        dispatch.mark_sent(sent['id'])
        storage.game.change_game_state('finish')
        storage.game.change_game_state('finish')
        self.assertTrue(storage.game.is_game_finished())
        self.assertTrue(storage.game.has_pending_checks())
        self.assertFalse(dispatch.touch_job(unpublished['id']))
        self.assertEqual(dispatch.pending_jobs(run['id']), [])
        self.assertTrue(dispatch.touch_job(sent['id']))
        self.fake_redis.flushdb()
        self.assertTrue(storage.game.is_game_finished())
        self.assertFalse(storage.game.set_game_running(True))
        self.assertIsNone(self.plan_round())
        for action in ('pause', 'resume'):
            with self.assertRaisesRegex(ValueError, 'already finished'):
                storage.game.change_game_state(action)
        storage.tasks.update_task_status(
            1, sent['payload']['team']['id'], 1, verdict(), sent['id'],
        )
        self.assertFalse(storage.game.has_pending_checks())
        with patch.object(storage.utils.SIOManager, 'write_only') as publisher:
            storage.game.finalize_finished_game()
        self.assertEqual(publisher.return_value.emit.call_count, 1)
        final = storage.game.get_cached_game_state()
        self.assertEqual(final.round, 1)
        self.assertEqual(sum(cell['checks'] for cell in final.team_tasks), 1)

    def test_finished_game_rejects_accounting_even_when_pause_cache_is_lost(self):
        self.plan_round()
        storage.game.change_game_state('finish')
        self.fake_redis.flushdb()
        flag = SimpleNamespace(team_id=2, task_id=1, round=1, id=42)
        with patch.object(storage.flags, 'get_flag_by_str', return_value=flag), \
                patch.object(storage.flags, 'try_add_stolen_flag', return_value=True), \
                patch.object(storage.game, 'get_current_game_config', return_value=SimpleNamespace(
                    flag_lifetime=5, volga_attacks_mode=False,
                )):
            result = storage.attacks.handle_attack(1, 'test-flag', 1)
        self.assertFalse(result.submit_ok)
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT score FROM TeamTasks ORDER BY team_id')
            self.assertEqual(cursor.fetchall(), [(2500,), (2500,)])

    def test_game_cannot_finish_before_first_round(self):
        with self.assertRaisesRegex(ValueError, 'not started'):
            storage.game.change_game_state('finish')
        self.assertFalse(storage.game.is_game_finished())

    def test_finish_preserves_worker_started_before_publication_ack(self):
        run = self.plan_round()
        job = dispatch.pending_jobs(run['id'])[0]
        self.assertTrue(dispatch.touch_job(job['id']))
        storage.game.change_game_state('finish')
        self.assertTrue(storage.game.has_pending_checks())
        self.assertTrue(storage.tasks.update_task_status(
            1, job['payload']['team']['id'], 1, verdict(), job['id'],
        ))
        self.assertFalse(storage.game.has_pending_checks())

    def test_pause_during_flag_validation_releases_reservation_without_scoring(self):
        self.plan_round()
        flag = SimpleNamespace(team_id=2, task_id=1, round=1, id=42)
        key = CacheKeys.team_stolen_flags(1)

        def reserve_then_pause(**kwargs):
            self.fake_redis.sadd(key, flag.id)
            storage.game.change_game_state('pause')
            return True

        with patch.object(storage.flags, 'get_flag_by_str', return_value=flag), \
                patch.object(storage.flags, 'try_add_stolen_flag', side_effect=reserve_then_pause), \
                patch.object(storage.game, 'get_current_game_config', return_value=SimpleNamespace(
                    flag_lifetime=5, volga_attacks_mode=False,
                )):
            result = storage.attacks.handle_attack(1, 'test-flag', 1)
        self.assertFalse(result.submit_ok)
        self.assertFalse(self.fake_redis.sismember(key, flag.id))
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT score FROM TeamTasks ORDER BY team_id')
            self.assertEqual(cursor.fetchall(), [(2500,), (2500,)])

    def test_only_a_new_game_can_be_started(self):
        with storage.utils.db_cursor() as (connection, cursor):
            cursor.execute('UPDATE GameConfig SET game_running=FALSE WHERE id=1')
            connection.commit()
        self.assertTrue(storage.game.set_game_running(True))
        self.assertFalse(storage.game.set_game_running(True))
        self.plan_round()
        storage.game.change_game_state('finish')
        self.assertFalse(storage.game.set_game_running(True))

    def test_partial_publication_resumes_same_round_and_job_ids(self):
        planned = self.plan_round()
        original = dispatch.pending_jobs(planned['id'])
        dispatch.mark_sent(original[0]['id'])
        resumed = self.plan_round()
        self.assertEqual(resumed['id'], planned['id'])
        self.assertEqual(storage.game.get_real_round_from_db(), 1)
        self.assertEqual(
            [job['id'] for job in dispatch.pending_jobs(resumed['id'])], [original[1]['id']],
        )

    def test_duplicate_result_counts_once_and_round_waits_for_both_teams(self):
        run = self.plan_round()
        jobs = dispatch.pending_jobs(run['id'])
        for job in jobs:
            dispatch.mark_sent(job['id'])
        dispatch.finish(run['id'], datetime.now(timezone.utc))
        self.assertIsNone(self.plan_round())
        first = jobs[0]
        first_team = first['payload']['team']['id']
        for _ in range(2):
            storage.tasks.update_task_status(1, first_team, 1, verdict(), first['id'])
        self.assertIsNone(self.plan_round())
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT checks,checks_passed FROM TeamTasks WHERE team_id=%s', (first_team,))
            self.assertEqual(cursor.fetchone(), (1, 1))
        last = jobs[1]
        storage.tasks.update_task_status(1, last['payload']['team']['id'], 1, verdict(), last['id'])
        next_run = self.plan_round()
        self.assertEqual(next_run['round'], 2)
        self.assertTrue(all(cell['checks'] == 1 for cell in next_run['snapshot']))

    def test_dispatch_checkpoint_and_completion_commit_together(self):
        run = self.plan_round()
        for job in dispatch.pending_jobs(run['id']):
            dispatch.mark_sent(job['id'])
        when = datetime.now(timezone.utc)
        dispatch.finish(run['id'], when)
        self.assertEqual(schedules.get_last_run('classic_rounds'), when)
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT completed FROM DispatchRuns WHERE id=%s', (run['id'],))
            self.assertTrue(cursor.fetchone()[0])

    def test_unpublished_batch_cannot_advance_schedule_checkpoint(self):
        run = self.plan_round()
        with self.assertRaises(RuntimeError):
            dispatch.finish(run['id'], datetime.now(timezone.utc))
        self.assertIsNone(schedules.get_last_run('classic_rounds'))

    def test_empty_put_batch_has_no_checker_jobs(self):
        run = dispatch.prepare(
            'blitz_rounds', [team()], [task(puts=0)], advances_round=True, puts_only=True,
        )
        self.assertEqual(dispatch.pending_jobs(run['id']), [])

    def test_migration_is_repeatable_and_preserves_scores(self):
        migrate()
        migrate()
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT token FROM Teams ORDER BY id')
            self.assertEqual(cursor.fetchall(), [(f'{i:016x}',) for i in (1, 2)])
            cursor.execute('SELECT score FROM TeamTasks ORDER BY team_id')
            self.assertEqual(cursor.fetchall(), [(2500.0,), (2500.0,)])

    def test_failed_accounting_transaction_does_not_complete_job(self):
        run = self.plan_round()
        job = dispatch.pending_jobs(run['id'])[0]
        with self.assertRaises(RuntimeError):
            with storage.utils.db_cursor() as (_, cursor):
                self.assertTrue(dispatch.claim_result(cursor, job['id']))
                raise RuntimeError('Rollback an interrupted update')
        with storage.utils.db_cursor() as (connection, cursor):
            self.assertTrue(dispatch.claim_result(cursor, job['id']))
            connection.commit()

    def published_round(self):
        run = self.plan_round()
        jobs = dispatch.pending_jobs(run['id'])
        for job in jobs:
            dispatch.mark_sent(job['id'])
        dispatch.finish(run['id'], datetime.now(timezone.utc))
        return jobs

    def expire_deadlines(self):
        with storage.utils.db_cursor() as (connection, cursor):
            cursor.execute(
                "UPDATE CheckerJobs SET deadline_at='-infinity' "
                'WHERE sent_at IS NOT NULL AND finished_at IS NULL',
            )
            connection.commit()

    def test_lost_deliveries_release_round_without_sla_or_score_penalty(self):
        self.published_round()
        self.assertIsNone(self.plan_round())
        self.expire_deadlines()
        recover_expired_jobs()
        run = self.plan_round()
        self.assertEqual(run['round'], 2)
        for cell in run['snapshot']:
            self.assertEqual((cell['checks'], cell['checks_passed']), (0, 0))
            self.assertEqual(cell['score'], 2500)
            self.assertEqual(cell['status'], models.TaskStatus.CHECK_FAILED.value)

    def test_lost_callback_preserves_put_and_ignores_late_result_and_flag(self):
        jobs = self.published_round()
        job = jobs[0]
        team_id = job['payload']['team']['id']
        config = SimpleNamespace(flag_lifetime=5, round_time=60)
        with patch.object(storage.game, 'get_current_game_config', return_value=config):
            flag = models.Flag.generate('S', team_id, 1, 1)
            flag.public_flag_data = 'fixture'
            flag.private_flag_data = 'fixture'
            flag.vuln_number = 1
            self.assertIsNotNone(storage.flags.add_flag(flag, job_id=job['id']))
            self.expire_deadlines()
            recover_expired_jobs()
            self.assertFalse(dispatch.touch_job(job['id']))
            self.assertFalse(storage.tasks.update_task_status(
                1, team_id, 1, verdict(), job['id'],
            ))
            late = models.Flag.from_dict(flag.to_dict())
            late.id = None
            late.flag = 'L' * 31 + '='
            self.assertIsNone(storage.flags.add_flag(late, job_id=job['id']))
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT COUNT(*) FROM Flags')
            self.assertEqual(cursor.fetchone()[0], 1)
            cursor.execute('SELECT checks,checks_passed,score FROM TeamTasks ORDER BY team_id')
            self.assertEqual(cursor.fetchall(), [(0, 0, 2500), (0, 0, 2500)])
        self.assertEqual(self.plan_round()['round'], 2)

    def test_activity_renewal_wins_over_a_stale_recovery_candidate(self):
        job = self.published_round()[0]
        self.expire_deadlines()
        self.assertIn(job['id'], {item['id'] for item in dispatch.expired_jobs()})
        self.assertTrue(dispatch.touch_job(job['id']))
        failed = verdict(status=models.TaskStatus.CHECK_FAILED)
        self.assertFalse(storage.tasks.update_task_status(
            1, job['payload']['team']['id'], 1, failed, job['id'], expired_only=True,
        ))
        recover_expired_jobs()
        self.assertIsNone(self.plan_round())
        self.assertTrue(storage.tasks.update_task_status(
            1, job['payload']['team']['id'], 1, verdict(), job['id'],
        ))
        self.assertEqual(self.plan_round()['round'], 2)

    def test_recovery_leaves_fresh_and_unpublished_jobs_pending(self):
        run = self.plan_round()
        job = dispatch.pending_jobs(run['id'])[0]
        dispatch.mark_sent(job['id'])
        recover_expired_jobs()
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT COUNT(*) FROM CheckerJobs WHERE finished_at IS NULL')
            self.assertEqual(cursor.fetchone()[0], 2)
        self.assertEqual(len(dispatch.pending_jobs(run['id'])), 1)

    def test_concurrent_recovery_and_result_account_only_once(self):
        job = self.published_round()[0]
        self.expire_deadlines()

        def complete(index):
            expired = index % 2 == 0
            result = verdict(status=models.TaskStatus.CHECK_FAILED) if expired else verdict()
            return storage.tasks.update_task_status(
                1, job['payload']['team']['id'], 1, result, job['id'],
                expired_only=expired,
            )

        with ThreadPoolExecutor(max_workers=8) as executor:
            results = list(executor.map(complete, range(16)))
        self.assertEqual(sum(results), 1)
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute(
                'SELECT checks,checks_passed,score FROM TeamTasks WHERE team_id=%s',
                (job['payload']['team']['id'],),
            )
            self.assertIn(cursor.fetchone(), ((0, 0, 2500), (1, 1, 2500)))
            cursor.execute('SELECT COUNT(*) FROM TeamTasksLog')
            self.assertEqual(cursor.fetchone()[0], 1)

    def test_old_dispatch_schema_gains_deadlines_without_resetting_them(self):
        run = self.plan_round()
        job = dispatch.pending_jobs(run['id'])[0]
        dispatch.mark_sent(job['id'])
        with storage.utils.db_cursor() as (connection, cursor):
            cursor.execute('ALTER TABLE CheckerJobs DROP COLUMN deadline_at CASCADE')
            connection.commit()
        migrate()
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT deadline_at FROM CheckerJobs WHERE id=%s', (job['id'],))
            deadline = cursor.fetchone()[0]
            self.assertIsNotNone(deadline)
            cursor.execute('SELECT deadline_at FROM CheckerJobs WHERE sent_at IS NULL')
            self.assertEqual(cursor.fetchall(), [(None,)])
        migrate()
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT deadline_at FROM CheckerJobs WHERE id=%s', (job['id'],))
            self.assertEqual(cursor.fetchone()[0], deadline)

    def test_concurrent_delivery_counts_once_with_bounded_connection_pool(self):
        run = self.plan_round()
        job = dispatch.pending_jobs(run['id'])[0]

        def record(_):
            storage.tasks.update_task_status(
                1, job['payload']['team']['id'], 1, verdict(), job['id'],
            )

        with ThreadPoolExecutor(max_workers=8) as executor:
            list(executor.map(record, range(16)))
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute(
                'SELECT checks,checks_passed FROM TeamTasks WHERE team_id=%s',
                (job['payload']['team']['id'],),
            )
            self.assertEqual(cursor.fetchone(), (1, 1))

    def test_concurrent_flag_submission_credits_once_and_preserves_first_blood_ids(self):
        self.plan_round()
        config = SimpleNamespace(flag_lifetime=5, round_time=60, volga_attacks_mode=False)
        with patch.object(storage.game, 'get_current_game_config', return_value=config):
            flag = models.Flag.generate('S', 2, 1, 1)
            flag.public_flag_data = 'fixture'
            flag.private_flag_data = 'fixture'
            flag.vuln_number = 1
            storage.flags.add_flag(flag)

            def submit(_):
                return storage.attacks.handle_attack(1, flag.flag, 1)

            with ThreadPoolExecutor(max_workers=8) as executor:
                results = list(executor.map(submit, range(16)))
            self.assertEqual(sum(result.submit_ok for result in results), 1)

            # A lost Redis reservation must not permit a second SQL credit.
            self.fake_redis.delete(CacheKeys.team_stolen_flags(1))
            self.assertFalse(submit(0).submit_ok)
        with storage.utils.db_cursor() as (_, cursor):
            cursor.execute('SELECT score,stolen,lost FROM TeamTasks ORDER BY team_id')
            attacker, victim = cursor.fetchall()
            delta = 25 * sqrt(10)
            self.assertAlmostEqual(attacker[0], 2500 + delta)
            self.assertEqual(attacker[1:], (1, 0))
            self.assertAlmostEqual(victim[0], 2500 - delta)
            self.assertEqual(victim[1:], (0, 1))
            cursor.execute('SELECT attacker_id,victim_id FROM get_first_bloods()')
            self.assertEqual(cursor.fetchall(), [(1, 2)])

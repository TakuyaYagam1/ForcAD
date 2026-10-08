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
                    '(id,real_round,game_hardness,flag_lifetime,round_time,inflation) '
                    'VALUES (1,0,10,5,60,TRUE)',
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

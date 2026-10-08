"""Offline regressions for the v3 patch. Run with PYTHONPATH=backend."""
import unittest
from datetime import datetime, timedelta, timezone
from unittest.mock import MagicMock, patch

import regressions_fixes as fixes
from regressions_fixes import TEAM, TASK, CFG
from lib import models, storage
from lib.helpers import events
from services.ticker.models import Schedule, TickerState
from services.ticker.__main__ import sync_blitz_schedules, main


class V3RegressionTests(unittest.TestCase):
    setUp = fixes.RegressionTests.setUp
    login = fixes.RegressionTests.login

    def test_real_broker_publisher_surfaces_failure_after_retry(self):
        manager = storage.utils.ReliableKombuManager('amqp://guest:guest@localhost//', write_only=True)
        publisher = MagicMock(side_effect=OSError('broker down'))
        with patch.object(manager, '_producer_publish', return_value=publisher):
            with self.assertRaises(OSError):
                manager._publish({'method': 'emit'})
        self.assertEqual(publisher.call_count, 2)

    def test_failed_retry_keeps_refresh_pending(self):
        self.redis.set(events.PENDING_REFRESH, 'pending')
        with patch.object(events, 'init_scoreboard', side_effect=RuntimeError('broker down')):
            events.retry_scoreboard_refresh()
        self.assertEqual(self.redis.get(events.PENDING_REFRESH), 'pending')

    def test_create_returns_success_when_broadcast_fails_and_retries(self):
        self.login()
        saved = []

        def create(team):
            saved.append(team)
            return models.Team(**{**team.to_dict(), 'id': 9})

        with patch.object(storage.teams, 'create_team', side_effect=create), \
                patch.object(events, 'init_scoreboard', side_effect=RuntimeError('broker down')):
            response = self.client.post('/api/admin/teams/', json=TEAM)
        self.assertEqual(response.status_code, 201)
        self.assertEqual(len(saved), 1)
        self.assertTrue(self.redis.get(events.PENDING_REFRESH))
        with patch.object(events, 'init_scoreboard') as emit:
            events.retry_scoreboard_refresh()
        emit.assert_called_once_with(refresh_state=True)
        self.assertFalse(self.redis.exists(events.PENDING_REFRESH))

    def test_task_update_and_delete_succeed_when_broadcast_fails(self):
        self.login()
        task = models.Task(id=1, **TASK)
        with patch.object(storage.tasks, 'get_all_tasks', return_value=[task]), \
                patch.object(storage.tasks, 'update_task', return_value=task), \
                patch.object(storage.tasks, 'delete_task') as delete, \
                patch.object(events, 'init_scoreboard', side_effect=RuntimeError('broker down')):
            self.assertEqual(self.client.put('/api/admin/tasks/1/', json=TASK).status_code, 200)
            self.assertEqual(self.client.delete('/api/admin/tasks/1/').status_code, 200)
        delete.assert_called_once_with(1)
        self.assertTrue(self.redis.get(events.PENDING_REFRESH))

    def test_retry_does_not_erase_a_newer_pending_refresh(self):
        self.redis.set(events.PENDING_REFRESH, 'older')
        with patch.object(events, 'init_scoreboard', side_effect=lambda **_: self.redis.set(events.PENDING_REFRESH, 'newer')):
            events.retry_scoreboard_refresh()
        self.assertEqual(self.redis.get(events.PENDING_REFRESH), 'newer')

    def test_pause_preserves_remaining_interval_and_survives_restart(self):
        schedule = Schedule('classic_rounds', datetime.fromtimestamp(0, timezone.utc), lambda **_: None,
                            last_run=datetime.fromtimestamp(1000, timezone.utc), interval=timedelta(seconds=60))
        with patch('lib.storage.game.time.time', return_value=1030):
            storage.game.set_game_paused(True)
        self.assertEqual(storage.game.get_scheduler_time(1500), 1030)
        with patch('lib.storage.game.time.time', return_value=1630):
            storage.game.set_game_paused(False)
            storage.game.set_game_paused(False)
        def due(wall):
            return schedule.should_be_called(datetime.fromtimestamp(storage.game.get_scheduler_time(wall), timezone.utc))
        self.assertFalse(due(1630))
        self.assertFalse(due(1659))
        self.assertTrue(due(1660))
        # Next round resets only its own counter. A restarted ticker uses the
        # persisted logical last_run and cumulative offset.
        self.redis.set(storage.game.PAUSED_SECONDS, 0)
        restarted = Schedule('classic_rounds', schedule.start, schedule.func, interval=schedule.interval)
        with patch.object(storage.schedules, 'get_last_run', return_value=datetime.fromtimestamp(1060, timezone.utc)):
            restarted.load_last_run()
        self.assertFalse(restarted.should_be_called(datetime.fromtimestamp(storage.game.get_scheduler_time(1690), timezone.utc)))
        self.assertTrue(restarted.should_be_called(datetime.fromtimestamp(storage.game.get_scheduler_time(1720), timezone.utc)))

    def test_multiple_pauses_do_not_advance_scheduler_clock(self):
        for at, paused in [(100, True), (120, False), (150, True), (190, False)]:
            with patch('lib.storage.game.time.time', return_value=at):
                storage.game.set_game_paused(paused)
        self.assertEqual(storage.game.get_scheduler_time(200), 140)
        self.assertEqual(float(self.redis.get(storage.game.PAUSED_SECONDS)), 60)

    def test_ticker_started_while_paused_does_not_execute_due_round(self):
        self.redis.set(storage.game.PAUSED_AT, 100)
        callback = MagicMock()
        state = TickerState(MagicMock(), True, [
            Schedule('classic_rounds', datetime.fromtimestamp(0, timezone.utc), callback),
        ])
        with patch('services.ticker.__main__.sync_blitz_schedules'), \
                patch.object(events, 'retry_scoreboard_refresh'), \
                patch('services.ticker.__main__.recover_expired_jobs') as recover, \
                patch('services.ticker.__main__.time.sleep', side_effect=InterruptedError):
            with self.assertRaises(InterruptedError):
                main(state)
        callback.assert_not_called()
        recover.assert_called_once_with()

    def test_blitz_schedules_add_update_disable_and_reactivate(self):
        cfg = models.GameConfig(**{**CFG, 'mode': 'blitz'})
        task = models.Task(id=1, **TASK)
        round_schedule = Schedule('blitz_rounds', cfg.start_time, lambda **_: None)
        state = TickerState(MagicMock(), True, [round_schedule])
        with patch.object(storage.game, 'get_current_game_config', return_value=cfg), \
                patch.object(storage.schedules, 'get_last_run', return_value=None), \
                patch.object(storage.tasks, 'get_tasks', return_value=[task]):
            sync_blitz_schedules(state)
            original = state.schedules[1]
            original.last_run = cfg.start_time
            task.get_period = 25
            sync_blitz_schedules(state)
            self.assertIs(state.schedules[1], original)
            self.assertEqual(original.interval.total_seconds(), 25)
            self.assertEqual(original.last_run, cfg.start_time)
            task2 = models.Task(id=2, **TASK)
            with patch.object(storage.tasks, 'get_tasks', return_value=[task, task2]):
                sync_blitz_schedules(state)
            self.assertEqual(len(state.schedules), 3)
            with patch.object(storage.tasks, 'get_tasks', return_value=[task2]):
                sync_blitz_schedules(state)
            self.assertNotIn('blitz_check_gets_task_1', [s.schedule_id for s in state.schedules])
            sync_blitz_schedules(state)
            self.assertIn('blitz_check_gets_task_1', [s.schedule_id for s in state.schedules])
            self.assertIs(state.schedules[0], round_schedule)

    def test_completed_history_includes_score_change_after_last_check(self):
        task = models.Task(id=1, **TASK)
        before = dict(team_id=1, task_id=1, round=3, status=101, score=2500,
                      checks=1, checks_passed=1, stolen=0, lost=0,
                      public_message='OK', private_message='secret', command='secret')
        self.redis.xadd(storage.keys.CacheKeys.teamtasks(1, 1), before)
        final = {**before, 'score': 2700, 'stolen': 1}
        final.pop('round')
        public = storage.tasks.filter_teamtasks_for_participants([final])[0]
        state = models.GameState(round=3, round_start=1000, team_tasks=[public])
        with patch.object(storage.game, 'construct_game_state_from_db', return_value=state), \
                patch.object(storage.utils.SIOManager, 'write_only', return_value=MagicMock()):
            storage.game.update_game_state(3)
        with patch.object(storage.tasks, 'get_tasks', return_value=[task]):
            response = self.client.get('/api/client/teams/1/')
        self.assertEqual(response.status_code, 200)
        rows = response.json
        self.assertEqual(len(rows), 1)
        self.assertEqual(float(rows[0]['score']), 2700)
        self.assertEqual(int(rows[0]['stolen']), 1)
        self.assertEqual(rows[0]['message'], 'OK')
        self.assertNotIn('command', rows[0])
        self.assertNotIn('private_message', rows[0])
        # Checker state remains separate for volga mode's status checks.
        self.assertEqual(float(storage.tasks.get_latest_teamtask(1, 1)['score']), 2500)

    def test_history_keeps_legacy_and_unfinished_rounds(self):
        task = models.Task(id=1, **TASK)
        for r in (1, 2, 3):
            self.redis.xadd(storage.keys.CacheKeys.teamtasks(1, 1), {'round': r, 'team_id': 1, 'task_id': 1, 'score': 100})
        self.redis.xadd(storage.keys.CacheKeys.teamtasks_history(1, 1), {'round': 2, 'team_id': 1, 'task_id': 1, 'score': 200})
        with patch.object(storage.tasks, 'get_tasks', return_value=[task]):
            rows = storage.tasks.get_teamtasks_for_team(1)
        self.assertEqual({int(row['round']): float(row['score']) for row in rows}, {1: 100, 2: 200, 3: 100})

    def test_ctftime_ties_and_close_scores_match_frontend(self):
        teams = [models.Team(id=i, **{**TEAM, 'name': f'Team {i}'}) for i in (2, 1)]
        cells = [dict(team_id=i, task_id=1, score=100, checks=1, checks_passed=1) for i in (2, 1)]
        state = models.GameState(round=3, round_start=1000, team_tasks=cells)
        with patch.object(storage.game, 'get_cached_game_state', return_value=state), \
                patch.object(storage.teams, 'get_teams', return_value=teams):
            self.assertEqual([r['team'] for r in storage.game.construct_ctftime_scoreboard()], ['Team 1', 'Team 2'])
            cells[0]['score'] = 100.004
            self.assertEqual([r['team'] for r in storage.game.construct_ctftime_scoreboard()], ['Team 2', 'Team 1'])


if __name__ == '__main__':
    suite = unittest.TestSuite([
        unittest.defaultTestLoader.loadTestsFromTestCase(fixes.RegressionTests),
        unittest.defaultTestLoader.loadTestsFromTestCase(V3RegressionTests),
    ])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    raise SystemExit(not result.wasSuccessful())

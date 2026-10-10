"""Flag validation remains correct while avoiding redundant Redis exchanges."""

import sys
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace
from unittest import TestCase
from unittest.mock import MagicMock, patch

import fakeredis

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'backend'))

from lib import models, storage
from lib.flags.judge import Judge
from lib.storage.keys import CacheKeys


class FlagCacheTests(TestCase):
    def setUp(self):
        self.redis = fakeredis.FakeRedis(decode_responses=True)
        self.addCleanup(self.redis.close)
        redis_patch = patch.object(
            storage.utils.RedisStorage, 'get', return_value=self.redis,
        )
        redis_patch.start()
        self.addCleanup(redis_patch.stop)
        self.config = models.GameConfig(
            flag_lifetime=5, game_hardness=10, inflation=True,
            volga_attacks_mode=False, round_time=60, mode='classic',
            timezone='UTC', start_time='2026-10-10T10:00:00+00:00',
            real_round=10, game_running=True,
        )
        self.flag = models.Flag(
            id=7, team_id=2, task_id=1, round=9, flag='S' * 31 + '=',
            public_flag_data='public', private_flag_data='private', vuln_number=1,
        )
        self.redis.set(CacheKeys.game_config(), self.config.to_json())
        self.redis.set(CacheKeys.current_round(), 10)
        self.redis.set(CacheKeys.flags_cached(), 1)
        self.redis.set(CacheKeys.flag_by_str(self.flag.flag), self.flag.to_json())
        self.redis.set(CacheKeys.flag_by_id(self.flag.id), self.flag.to_json())
        self.redis.sadd(CacheKeys.team_stolen_flags(1), self.flag.id)
        self.judge = Judge.__new__(Judge)
        self.judge._monitor = MagicMock()
        self.judge._notifier = MagicMock()

    @contextmanager
    def redis_exchanges(self):
        conn = self.redis.connection_pool.get_connection()
        connection_type = type(conn)
        self.redis.connection_pool.release(conn)
        send = connection_type.send_packed_command
        counts = SimpleNamespace(total=0)

        def counted_send(connection, *args, **kwargs):
            counts.total += 1
            return send(connection, *args, **kwargs)

        with patch.object(connection_type, 'send_packed_command', counted_send):
            yield counts

    def test_warm_config_uses_one_exchange_and_observes_changes(self):
        with patch.object(storage.game, 'get_db_game_config') as database:
            with self.redis_exchanges() as count:
                loaded = storage.game.get_current_game_config()
            self.assertEqual(count.total, 1)
            self.assertEqual(loaded.flag_lifetime, 5)
            self.config.flag_lifetime = 7
            self.redis.set(CacheKeys.game_config(), self.config.to_json())
            self.assertEqual(storage.game.get_current_game_config().flag_lifetime, 7)
            database.assert_not_called()

    def test_evicted_config_is_restored_from_database(self):
        self.redis.delete(CacheKeys.game_config())
        with patch.object(
            storage.game, 'get_db_game_config', return_value=self.config,
        ) as database:
            first = storage.game.get_current_game_config()
            second = storage.game.get_current_game_config()
        self.assertEqual(first.to_dict(), second.to_dict())
        self.assertEqual(first.to_dict(), self.config.to_dict())
        database.assert_called_once_with()

    def test_warm_flag_lookup_uses_one_exchange_without_database(self):
        with patch.object(storage.utils, 'db_cursor') as database:
            with self.redis_exchanges() as count:
                loaded = storage.flags.get_flag_by_str(self.flag.flag, 10)
        self.assertEqual(loaded.to_dict(), self.flag.to_dict())
        self.assertEqual(count.total, 1)
        database.assert_not_called()

    def test_missing_marker_still_restores_flag_cache(self):
        self.redis.delete(CacheKeys.flags_cached())
        cursor = MagicMock()
        updated = models.Flag.from_dict({**self.flag.to_dict(), 'round': 10})
        cursor.fetchall.return_value = [updated.to_dict()]

        @contextmanager
        def database(**_):
            yield MagicMock(), cursor

        with patch.object(storage.utils, 'db_cursor', database):
            loaded = storage.flags.get_flag_by_str(self.flag.flag, 10)
        self.assertEqual(loaded.round, 10)
        self.assertTrue(self.redis.exists(CacheKeys.flags_cached()))
        cursor.execute.assert_called_once()

    def test_missing_flag_can_appear_in_database_on_next_lookup(self):
        self.redis.delete(CacheKeys.flag_by_str(self.flag.flag))
        cursor = MagicMock()
        cursor.fetchone.side_effect = [None, self.flag.to_dict()]

        @contextmanager
        def database(**_):
            yield MagicMock(), cursor

        with patch.object(storage.utils, 'db_cursor', database):
            self.assertIsNone(storage.flags.get_flag_by_str(self.flag.flag, 10))
            loaded = storage.flags.get_flag_by_str(self.flag.flag, 10)
        self.assertEqual(loaded.id, self.flag.id)
        self.assertEqual(cursor.execute.call_count, 2)
        self.assertGreater(self.redis.ttl(CacheKeys.flag_by_str(self.flag.flag)), 0)

    def test_cached_duplicate_batch_has_bounded_redis_exchanges(self):
        with patch.object(storage.utils, 'db_cursor') as database:
            with self.redis_exchanges() as count:
                results = self.judge.process_many(1, [self.flag.flag] * 100)
        self.assertEqual(len(results), 100)
        self.assertTrue(all(not result.submit_ok for result in results))
        self.assertTrue(all('already stolen' in result.message for result in results))
        self.assertLessEqual(count.total, 600)
        database.assert_not_called()
        self.judge._notifier.add.assert_not_called()

    def test_concurrent_reservations_have_one_winner(self):
        new_flag = SimpleNamespace(id=8)
        with ThreadPoolExecutor(max_workers=8) as executor:
            results = list(executor.map(
                lambda _: storage.flags.try_add_stolen_flag(new_flag, 1, 10),
                range(8),
            ))
        self.assertEqual(sum(results), 1)
        self.assertTrue(self.redis.sismember(CacheKeys.team_stolen_flags(1), 7))

    def test_batch_rechecks_round_for_each_flag(self):
        with patch.object(storage.game, 'get_real_round', side_effect=[10, 15]):
            results = self.judge.process_many(1, [self.flag.flag] * 2)
        self.assertIn('already stolen', results[0].message)
        self.assertIn('too old', results[1].message)
        self.assertTrue(all(not result.submit_ok for result in results))

    def test_batch_stops_accepting_flags_after_pause(self):
        reserve = storage.flags.try_add_stolen_flag

        def reserve_then_pause(*args, **kwargs):
            result = reserve(*args, **kwargs)
            self.redis.set(storage.game.PAUSED_AT, 1)
            return result

        with patch.object(
            storage.flags, 'try_add_stolen_flag', side_effect=reserve_then_pause,
        ):
            results = self.judge.process_many(1, [self.flag.flag] * 2)
        self.assertIn('already stolen', results[0].message)
        self.assertIn('not available', results[1].message)
        self.assertTrue(all(not result.submit_ok for result in results))

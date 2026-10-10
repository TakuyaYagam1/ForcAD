"""Database connection reuse under concurrent receiver traffic."""

import sys
from pathlib import Path
from unittest import TestCase
from unittest.mock import MagicMock, patch

from psycopg2 import extensions

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'backend'))

from lib.storage import utils


def connection():
    conn = MagicMock()
    conn.closed = False
    conn.info.transaction_status = extensions.TRANSACTION_STATUS_INTRANS
    return conn


class DatabasePoolTests(TestCase):
    def create_pool(self, service):
        settings = MagicMock()
        settings.model_dump.return_value = {}
        with patch.dict('os.environ', {'SERVICE': service}), patch.object(
            utils.config, 'get_db_config', return_value=settings,
        ):
            db_pool = utils.DBPool.create()
        self.addCleanup(db_pool.closeall)
        return db_pool

    @patch('psycopg2.connect', side_effect=lambda **_: connection())
    def test_receiver_reuses_connections_across_full_pool_bursts(self, connect):
        db_pool = self.create_pool('http_receiver')
        first = [db_pool.getconn() for _ in range(20)]
        for conn in first:
            db_pool.putconn(conn)

        second = [db_pool.getconn() for _ in range(20)]
        self.assertEqual({id(conn) for conn in first}, {id(conn) for conn in second})
        self.assertEqual(connect.call_count, 20)
        for conn in second:
            db_pool.putconn(conn)
            self.assertFalse(conn.close.called)
            self.assertEqual(conn.rollback.call_count, 2)

    @patch('psycopg2.connect', side_effect=lambda **_: connection())
    def test_other_services_do_not_preallocate_twenty_connections(self, connect):
        for service in ('worker', 'api', 'admin', 'events', 'ticker', ''):
            with self.subTest(service=service):
                before = connect.call_count
                db_pool = self.create_pool(service)
                first = db_pool.getconn()
                db_pool.putconn(first)
                second = db_pool.getconn()
                self.assertIs(first, second)
                db_pool.putconn(second)
                self.assertEqual(connect.call_count - before, 1)

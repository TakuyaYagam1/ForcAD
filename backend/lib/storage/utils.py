import json
import os
from contextlib import contextmanager
from threading import BoundedSemaphore, Lock

import kombu
import redis
import socketio
from psycopg2 import extras, pool

from lib import config
from lib.helpers.singleton import Singleton


class ConnectionPool(pool.ThreadedConnectionPool):
    """Bound concurrent SQL work without exhausting the connection pool."""

    def __init__(self, minconn, maxconn, **kwargs):
        super().__init__(minconn, maxconn, **kwargs)
        self._slots = BoundedSemaphore(maxconn)

    def getconn(self, key=None):
        if not self._slots.acquire(timeout=30):
            raise pool.PoolError('Database connection wait timed out')
        try:
            return super().getconn(key)
        except BaseException:
            self._slots.release()
            raise

    def putconn(self, conn, key=None, close=False):
        try:
            super().putconn(conn, key, close)
        finally:
            self._slots.release()


class DBPool(Singleton[ConnectionPool]):

    @staticmethod
    def create() -> ConnectionPool:
        database_config = config.get_db_config()
        # psycopg2 closes returned connections once minconn idle slots are full.
        # Keep the receiver's bounded pool warm between concurrent flag batches.
        minconn = 20 if os.getenv('SERVICE') == 'http_receiver' else 1
        return ConnectionPool(
            minconn=minconn,
            maxconn=20,
            **database_config.model_dump(),
        )


class RedisStorage(Singleton[redis.Redis]):

    @staticmethod
    def create() -> redis.Redis:
        redis_config = config.get_redis_config()
        return redis.Redis(decode_responses=True, **redis_config.model_dump())


class SIOManager(Singleton[socketio.KombuManager]):

    @staticmethod
    def create(*, write_only: bool = False) -> socketio.KombuManager:
        broker_url = config.get_broker_url()
        return LockedKombuManager(
            url=broker_url,
            write_only=write_only,
            channel='forcad-front',
            # Socket.IO subscriptions are private to this connection.
            # RabbitMQ 4.3 rejects non-exclusive transient queues.
            queue_options={'exclusive': True, 'auto_delete': True},
        )

    @classmethod
    def get(cls, *, write_only: bool) -> socketio.KombuManager:
        return super().get(write_only=write_only)

    @classmethod
    def write_only(cls) -> socketio.KombuManager:
        return cls.get(write_only=True)

    @classmethod
    def read_write(cls) -> socketio.KombuManager:
        return cls.get(write_only=False)

    @staticmethod
    def reliable_write_only() -> socketio.KombuManager:
        return ReliableSIOManager.get()


class LockedKombuManager(socketio.KombuManager):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._publish_lock = Lock()

    def _publish(self, data):
        with self._publish_lock:
            return super()._publish(data)


class ReliableKombuManager(LockedKombuManager):
    """Surface exhausted publish failures instead of silently losing a refresh.

    Socket.IO's standard KombuManager logs and swallows these failures. This
    manager is used only for recoverable scoreboard initialization broadcasts.
    """
    def _publish(self, data):
        payload = json.dumps(data)
        with self._publish_lock:
            for attempt in range(2):
                try:
                    self._producer_publish(self.publisher_connection)(payload)
                    return
                except (OSError, kombu.exceptions.KombuError):
                    if attempt:
                        raise


class ReliableSIOManager(Singleton[socketio.KombuManager]):
    @staticmethod
    def create() -> socketio.KombuManager:
        return ReliableKombuManager(
            url=config.get_broker_url(),
            write_only=True,
            channel='forcad-front',
        )


class BrokerConnection(Singleton[kombu.Connection]):

    @staticmethod
    def create() -> kombu.Connection:
        return kombu.Connection(config.get_broker_url())


@contextmanager
def db_cursor(dict_cursor: bool = False):  # type: ignore
    db_pool = DBPool.get()
    conn = db_pool.getconn()
    curs = None
    try:
        if dict_cursor:
            curs = conn.cursor(cursor_factory=extras.RealDictCursor)
        else:
            curs = conn.cursor()
        yield conn, curs
    except Exception:
        conn.rollback()
        raise
    finally:
        try:
            if curs is not None:
                curs.close()
        finally:
            db_pool.putconn(conn)


def redis_pipeline(transaction: bool = True) -> redis.client.Pipeline:
    storage = RedisStorage.get()
    return storage.pipeline(transaction=transaction)

#!/usr/bin/env python3

import time
from pathlib import Path

import psycopg2
import redis

from lib import config

BASE_DIR = Path(__file__).absolute().resolve().parents[1]
SCRIPTS_DIR = BASE_DIR / 'scripts'


def wait_for_storage(operation):
    for attempt in range(12):
        try:
            return operation()
        except (psycopg2.OperationalError, redis.exceptions.ConnectionError,
                redis.exceptions.TimeoutError, redis.exceptions.BusyLoadingError):
            if attempt == 11:
                raise RuntimeError('Storage unavailable after 12 attempts') from None
            print('[*] Storage unavailable, retrying in 5 seconds...')
            time.sleep(5)


def run():
    database = config.get_db_config().model_dump()
    conn = wait_for_storage(lambda: psycopg2.connect(**database, connect_timeout=5))
    try:
        with conn, conn.cursor() as curs:
            curs.execute("SET statement_timeout = '30s'")
            curs.execute((SCRIPTS_DIR / 'drop_query.sql').read_text())
    finally:
        conn.close()

    cache = redis.Redis(
        **config.get_redis_config().model_dump(),
        socket_connect_timeout=5, socket_timeout=5,
    )
    try:
        wait_for_storage(cache.flushall)
    finally:
        cache.close()


if __name__ == '__main__':
    run()

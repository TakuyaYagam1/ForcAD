#!/opt/forcad/bin/python

import sys

import kombu
import psycopg2

from forcad.lib.config.getters import get_broker_url, get_db_config


def main() -> int:
    database = None
    broker = None
    try:
        database = psycopg2.connect(**get_db_config().model_dump())
        broker = kombu.Connection(get_broker_url())
        broker.connect()
    except Exception:
        print('PostgreSQL or RabbitMQ is not ready.', file=sys.stderr)
        return 1
    finally:
        if database is not None:
            database.close()
        if broker is not None:
            broker.release()

    return 0


if __name__ == '__main__':
    raise SystemExit(main())

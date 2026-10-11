#!/opt/forcad/bin/python

import sys

import psycopg2

from forcad.lib.config.getters import get_db_config


def main() -> int:
    connection = None
    try:
        connection = psycopg2.connect(**get_db_config().model_dump())
        with connection.cursor() as cursor:
            cursor.execute('SELECT COUNT(id) from GameConfig')
            initialized, = cursor.fetchone()
    except psycopg2.ProgrammingError:
        return 1
    except Exception:
        print('Unable to check initialization.', file=sys.stderr)
        return 2
    finally:
        if connection is not None:
            connection.close()

    return 0 if initialized else 1


if __name__ == '__main__':
    raise SystemExit(main())

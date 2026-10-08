#!/usr/bin/env python3

from scripts import init_db, reset_db


def run():
    print('Resetting the database')
    reset_db.run()

    print('Initializing the database')
    init_db.run()

    print('Team tokens are available with ./control.py print_tokens')


if __name__ == '__main__':
    run()

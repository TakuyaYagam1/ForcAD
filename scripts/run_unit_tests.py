#!/usr/bin/env python3

import unittest
from pathlib import Path


TESTS_DIR = Path(__file__).resolve().parents[1] / 'tests'
OFFLINE_PATTERNS = (
    'test_auth_security.py',
    'test_checker_limits.py',
    'test_cleanup.py',
    'test_db_pool.py',
    'test_flag_cache.py',
    'test_game_control.py',
    'test_round_limits.py',
    'test_request_limits.py',
    'test_runtime.py',
    'test_team_tokens.py',
    'regressions_*.py',
)


def main() -> int:
    loader = unittest.TestLoader()
    suite = unittest.TestSuite()
    for pattern in OFFLINE_PATTERNS:
        suite.addTests(loader.discover(str(TESTS_DIR), pattern=pattern))

    result = unittest.TextTestRunner(verbosity=2).run(suite)
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())

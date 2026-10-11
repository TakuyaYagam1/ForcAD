#!/usr/bin/env python3
"""Run the HTTP game tests against an explicitly prepared test deployment."""

import os
from pathlib import Path
import subprocess
import sys

from dotenv import dotenv_values


def main() -> int:
    if os.getenv('TEST') != '1':
        raise SystemExit('Set TEST=1 and prepare the disposable test deployment.')
    root = Path(__file__).resolve().parents[1]
    runtime = root / 'config' / 'runtime'
    for name in ('postgres_environment.env', 'redis_environment.env',
                 'rabbitmq_environment.env', 'admin.env'):
        path = runtime / name
        if not path.is_file():
            raise SystemExit(f'Missing test environment: {path}')
        for key, value in dotenv_values(path, interpolate=False).items():
            if value is not None:
                os.environ[key] = value
    for pattern in ('test*.py', 'regressions_*.py'):
        result = subprocess.run(
            [sys.executable, '-m', 'unittest', 'discover', '-v',
             '-s', str(root / 'backend/tests'), '-p', pattern],
            cwd=root,
            check=False,
        )
        if result.returncode:
            return result.returncode
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

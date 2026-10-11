#!/usr/bin/env python3
"""Run SQLFluff without changing the platform's Click dependency."""

import argparse
from pathlib import Path
import subprocess
import sys
import tomllib


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--uv', default='uv')
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    with (root / 'backend/pyproject.toml').open('rb') as stream:
        config = tomllib.load(stream)
    requirement = config['tool']['forcad']['tools']['sqlfluff']
    result = subprocess.run([
        args.uv, 'tool', 'run', '--python', sys.executable, '--from', requirement,
        'sqlfluff', *args.arguments,
    ], cwd=root, check=False)
    return result.returncode


if __name__ == '__main__':
    raise SystemExit(main())

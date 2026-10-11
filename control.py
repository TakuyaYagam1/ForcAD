#!/usr/bin/env python3

import sys

try:
    from forcad.cli import main
except ModuleNotFoundError as error:
    if error.name != 'forcad':
        raise
    print(
        'Install the backend package, then run '
        '`uv run --project backend --locked --group cli forcad`.',
        file=sys.stderr,
    )
    raise SystemExit(1) from error


if __name__ == '__main__':
    main()

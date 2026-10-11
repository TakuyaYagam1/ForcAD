#!/usr/bin/env bash
set -euo pipefail

if [[ "${FORCAD_ALLOW_DESTRUCTIVE_TESTS:-}" != 1 ]]; then
    echo "Use a disposable checkout and set FORCAD_ALLOW_DESTRUCTIVE_TESTS=1." >&2
    exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
export TEST=1

python backend/tests/setup_forcad.py
forcad setup
forcad reset --full
forcad build
forcad start
forcad rd ps
forcad rd logs -f initializer
python backend/tests/wait_for_start.py
python scripts/run_game_tests.py

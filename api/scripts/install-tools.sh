#!/usr/bin/env bash
set -euo pipefail

api_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$api_root"
"${PYTHON:-python3}" -m venv .venv
.venv/bin/python -m pip install --only-binary=:all: -r scripts/requirements-codegen.txt
"${PNPM:-pnpm}" install --frozen-lockfile

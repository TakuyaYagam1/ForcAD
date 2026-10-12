#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
make --no-print-directory -s -C "$project_root/backend" build >&2
cd "$project_root"
exec "$project_root/backend/bin/forcad" control --project-root "$project_root" "$@"

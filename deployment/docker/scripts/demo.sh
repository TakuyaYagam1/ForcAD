#!/usr/bin/env bash
set -euo pipefail
umask 077
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
cd "$root"
if [[ "${1:-}" == up ]]; then
    if [[ ! -x api/.venv/bin/python ]]; then
        echo 'Run make -C backend sync-codegen first (the demo uses its PyYAML).' >&2
        exit 1
    fi
    api/.venv/bin/python deployment/docker/scripts/prepare_demo.py "$root"
    ./control.sh setup
    shift
    set -- up -d --build --wait --wait-timeout 180 "$@"
elif [[ $# == 0 ]]; then
    echo 'Usage: bash deployment/docker/scripts/demo.sh up|stop|start|down|ps|logs [Compose options]' >&2
    exit 2
fi
exec ./control.sh compose --env-file "$root/config/runtime/demo.env" \
    -f "$root/deployment/docker/compose.monitoring.yml" \
    -f "$root/deployment/docker/compose.demo.yml" --profile monitoring "$@"

#!/usr/bin/env bash
set -euo pipefail
umask 077
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
cd "$root"
compose() {
    ./control.sh compose --env-file "$root/config/runtime/demo.env" \
        -f "$root/deployment/docker/compose.monitoring.yml" \
        -f "$root/deployment/docker/compose.demo.yml" --profile monitoring "$@"
}
if [[ "${1:-}" == up ]]; then
    if [[ ! -x api/.venv/bin/python ]]; then
        echo 'Run make -C backend sync-codegen first (the demo uses its PyYAML).' >&2
        exit 1
    fi
    api/.venv/bin/python deployment/docker/scripts/prepare_demo.py "$root"
    ./control.sh setup
    shift
    compose config --quiet
    compose build
    # Namespace-sharing services must all be recreated with their new owner.
    # Keep database, notes, Prometheus and Grafana volumes.
    compose down --timeout 15
    compose up -d --wait --wait-timeout 180 "$@"
    api/.venv/bin/python deployment/docker/scripts/sync_demo.py "$root"
    exit
elif [[ "${1:-}" == phase ]]; then
    [[ $# == 2 && ( "$2" == game || "$2" == pre-game ) ]] || { echo 'Usage: demo.sh phase game|pre-game' >&2; exit 2; }
    compose exec -T demo-router nft -c -f "/etc/forcad/router/$2.nft"
    compose exec -T demo-router nft -f "/etc/forcad/router/$2.nft"
    printf '%s\n' "$2" > config/runtime/demo/phase
    echo "Demo network phase: $2"
    exit
elif [[ "${1:-}" == load ]]; then
    team="${2:-}"
    [[ "$team" == 1 || "$team" == 2 ]] || { echo 'Usage: demo.sh load 1|2 [--rps 20 --seconds 45 --ws 5]' >&2; exit 2; }
    shift 2
    compose exec -T "demo-farm$team" python load.py "$@"
    exit
elif [[ "${1:-}" == test ]]; then
    exec api/.venv/bin/python deployment/demo/check.py
elif [[ $# == 0 ]]; then
    echo 'Usage: demo.sh up|stop|start|down|ps|logs|test|phase|load [options]' >&2
    exit 2
fi
compose "$@"

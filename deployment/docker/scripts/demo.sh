#!/usr/bin/env bash
set -euo pipefail
umask 077
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
state="$root/.demo"
export FORCAD_RUNTIME_DIR="$state/config/runtime"
cd "$root"
compose() {
    docker compose --project-name forcad-demo --project-directory "$root/deployment/docker" \
        --env-file "$state/compose.env" -f "$root/deployment/docker/compose.yml" \
        -f "$root/deployment/docker/compose.monitoring.yml" \
        -f "$root/deployment/docker/compose.demo.yml" \
        -f "$state/network/compose.yml" --profile monitoring "$@"
}
case "${1:-}" in
    up|start|stop|restart|down)
        for arg in "${@:2}"; do
            case "$arg" in
                -p*|-f*|--project-name*|--project-directory*|--file*|--env-file*)
                    echo "The demo owns its Compose project and files" >&2; exit 2 ;;
            esac
        done ;;
esac
if [[ "${1:-}" == up ]]; then
    if [[ ! -x api/.venv/bin/python ]]; then
        echo 'Run make -C backend sync-codegen first (the demo uses its PyYAML).' >&2
        exit 1
    fi
    api/.venv/bin/python deployment/docker/scripts/prepare_demo.py "$root"
    make --no-print-directory -s -C backend build
    backend/bin/forcad control --project-root "$state" setup
    shift
    compose config --format json | python3 deployment/docker/scripts/demo_routes.py
    compose build
    # Namespace-sharing services must all be recreated with their new owner.
    # Keep database, notes, Prometheus and Grafana volumes.
    compose down --timeout 15
    compose up -d --wait --wait-timeout 180 "$@"
    binding="$(compose port demo-forcad 8080)"
    api/.venv/bin/python deployment/docker/scripts/sync_demo.py "$state" "${binding#127.0.0.1:}"
    exit
elif [[ "${1:-}" == phase ]]; then
    [[ $# == 2 && ( "$2" == game || "$2" == pre-game ) ]] || { echo 'Usage: demo.sh phase game|pre-game' >&2; exit 2; }
    compose exec -T demo-router nft -c -f "/etc/forcad/router/$2.nft"
    compose exec -T demo-router nft -f "/etc/forcad/router/$2.nft"
    printf '%s\n' "$2" > "$state/network/phase"
    echo "Demo network phase: $2"
    exit
elif [[ "${1:-}" == load ]]; then
    team="${2:-}"
    if [[ ! "$team" =~ ^[1-9][0-9]*$ ]] || ! compose config --services | grep -x "demo-farm$team" >/dev/null; then
        echo 'Usage: demo.sh load TEAM_NUMBER [--rps 20 --seconds 45 --ws 5]' >&2; exit 2;
    fi
    shift 2
    compose exec -T "demo-farm$team" python load.py "$@"
    exit
elif [[ "${1:-}" == test ]]; then
    exec api/.venv/bin/python deployment/demo/check.py
elif [[ ! "${1:-}" =~ ^(start|stop|down|ps|logs|exec|port|config|restart)$ ]]; then
    echo 'Usage: demo.sh up|stop|start|down|ps|logs|test|phase|load [options]' >&2
    exit 2
fi
compose "$@"

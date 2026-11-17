#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)"
case "${1:-checker}" in
    checker)
        export LOAD_PROFILE=checker LOAD_REST_RPS=100
        export LOAD_DURATION="${LOAD_DURATION:-15m}"
        ;;
    combined)
        export LOAD_PROFILE=load LOAD_REST_RPS=10000
        export LOAD_DURATION="${LOAD_DURATION:-15m}"
        ;;
    soak)
        export LOAD_PROFILE=soak LOAD_REST_RPS=10000
        export LOAD_DURATION="${LOAD_DURATION:-5h}"
        ;;
    *) echo "Usage: $0 {checker|combined|soak}" >&2; exit 2 ;;
esac
export LOAD_CAPACITY=chrp-final LOAD_MONITORING=1 LOAD_DIAGNOSTICS=1
export LOAD_TEAM_COUNT=28 LOAD_SPECTATORS=224
export LOAD_API_REPLICAS=4 LOAD_RECEIVER_REPLICAS=4 LOAD_WORKER_REPLICAS=20
export LOAD_WORKER_CONCURRENCY=8 LOAD_WORKER_DATABASE_MAX_CONNS=4
export LOAD_CHECKER_MEMORY_LIMIT_MB=1024
export LOAD_PUTS=14,13,13 LOAD_ROUND_TIME=60 LOAD_CHECKER_TIMEOUT=20
export LOAD_PFR=1
export LOAD_API_DATABASE_MAX_CONNS=16 LOAD_API_DATABASE_MIN_CONNS=16
export LOAD_API_HTTP_MAX_INFLIGHT=256 LOAD_API_GOGC=200
export LOAD_PGBOUNCER_API_CONNECTIONS=32 LOAD_PGBOUNCER_API_MIN_CONNECTIONS=32
export LOAD_DATABASE_STORAGE=disk LOAD_CONTAINER_LOGGING=local
export LOAD_CHECKPOINT_INTERVAL="$LOAD_DURATION"
exec make -C "$root/backend" test-load

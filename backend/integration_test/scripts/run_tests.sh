#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
test_project="${FORCAD_TEST_PROJECT:-forcad-integration-$$}"
if [[ ! "$test_project" =~ ^forcad-integration-[a-z0-9-]+$ ]]; then
    echo 'FORCAD_TEST_PROJECT must start with forcad-integration-.' >&2
    exit 2
fi
compose=(docker compose --project-name "$test_project" -f "$repo_root/deployment/docker/compose.integration.yml")
if [[ -n "$("${compose[@]}" ps -aq)" ]]; then
    echo "The test project $test_project already exists; refusing to reuse it." >&2
    exit 2
fi
cleanup() {
    result=$?
    if (( result != 0 )); then
        "${compose[@]}" logs --tail 100 || true
    fi
    "${compose[@]}" down --volumes --remove-orphans --rmi local || true
    exit "$result"
}
trap cleanup EXIT
# Docker assigns a new ephemeral published port on container restart. Reserve
# an explicit test port so reconnect tests keep the same broker endpoint.
export FORCAD_TEST_NATS_PORT="$(python3 - <<'PY'
import socket
with socket.socket() as listener:
    listener.bind(('127.0.0.1', 0))
    print(listener.getsockname()[1])
PY
)"
"${compose[@]}" up -d --build --wait --wait-timeout 90

postgres_port="$("${compose[@]}" port postgres 5432)"
redis_port="$("${compose[@]}" port redis 6379)"
nats_port="$("${compose[@]}" port nats 4222)"
nats_container="$("${compose[@]}" ps --status running -q nats)"
pooler_port="$("${compose[@]}" port pgbouncer 6432)"
export FORCAD_TEST_POOLER_ADDR="127.0.0.1:${pooler_port##*:}"
export FORCAD_TEST_DATABASE_URL="postgres://forcad_test:local-integration-password@127.0.0.1:${postgres_port##*:}/forcad_test?sslmode=disable"
export FORCAD_TEST_REDIS_ADDR="127.0.0.1:${redis_port##*:}"
export FORCAD_TEST_NATS_URL="nats://forcad_test:local-integration-password@127.0.0.1:${nats_port##*:}"
export FORCAD_TEST_NATS_PREFIX="forcad_test_${test_project//-/_}"
export FORCAD_TEST_NATS_CONTAINER="$nats_container"
export FORCAD_TEST_NATS_PROJECT="$test_project"

cd "$repo_root/backend"
go test -race -count=1 -p 1 "$@" ./integration_test/...

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
"${compose[@]}" up -d --build --wait --wait-timeout 90

postgres_port="$("${compose[@]}" port postgres 5432)"
redis_port="$("${compose[@]}" port redis 6379)"
rabbit_port="$("${compose[@]}" port rabbitmq 5672)"
pooler_port="$("${compose[@]}" port pgbouncer 6432)"
export FORCAD_TEST_POOLER_ADDR="127.0.0.1:${pooler_port##*:}"
export FORCAD_TEST_DATABASE_URL="postgres://forcad_test:local-integration-password@127.0.0.1:${postgres_port##*:}/forcad_test?sslmode=disable"
export FORCAD_TEST_REDIS_ADDR="127.0.0.1:${redis_port##*:}"
export FORCAD_TEST_AMQP_URL="amqp://forcad_test:local-integration-password@127.0.0.1:${rabbit_port##*:}/forcad_test"

cd "$repo_root/backend"
go test -race -count=1 -p 1 "$@" ./integration_test/...

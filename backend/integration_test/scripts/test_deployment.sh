#!/usr/bin/env bash
set -euo pipefail
umask 077
run_load=0
run_monitoring=0
api_replicas=2

case "${1:-}" in
    "") run_e2e=0 ;;
    --e2e)
        if (( $# != 1 )); then
            echo 'Usage: test_deployment.sh [--e2e|--load|--monitoring]' >&2
            exit 2
        fi
        run_e2e=1
        ;;
    --load)
        if (( $# != 1 )); then
            echo 'Usage: test_deployment.sh [--e2e|--load|--monitoring]' >&2
            exit 2
        fi
        run_e2e=0
        run_load=1
        ;;
    --monitoring)
        if (( $# != 1 )); then exit 2; fi
        run_e2e=0
        run_monitoring=1
        ;;
    *)
        echo 'Usage: test_deployment.sh [--e2e|--load|--monitoring]' >&2
        exit 2
        ;;
esac

if (( run_load )); then
    api_replicas="${LOAD_API_REPLICAS:-2}"
    if [[ ! "$api_replicas" =~ ^([1-9]|1[0-6])$ ]]; then
        echo 'LOAD_API_REPLICAS must be an integer between 1 and 16.' >&2
        exit 2
    fi
    case "${LOAD_DATABASE_STORAGE:-disk}" in
        disk|memory) ;;
        *) echo 'LOAD_DATABASE_STORAGE must be disk or memory.' >&2; exit 2 ;;
    esac
    case "${LOAD_CONTAINER_LOGGING:-default}" in
        default|local) ;;
        *) echo 'LOAD_CONTAINER_LOGGING must be default or local.' >&2; exit 2 ;;
    esac
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
tmp_parent="${TMPDIR:-/tmp}"
tmp_parent="$(cd "$tmp_parent" && pwd -P)"
test_root="$(mktemp -d "$tmp_parent/forcad-deployment.XXXXXX")"
chmod 0700 "$test_root"
test_project="forcad-deployment-$(id -u)-$$-$RANDOM"
image_prefix="$test_project"
runtime_env="$test_root/runtime.env"
compose=(docker compose --project-name "$test_project" --project-directory "$test_root/deployment/docker" -f "$test_root/deployment/docker/compose.yml" -f "$repo_root/deployment/docker/compose.e2e.yml")
image_tags=("$image_prefix-runtime" "$image_prefix-worker" "$image_prefix-nginx" "$image_prefix-postgres" "$image_prefix-pgbouncer")
if (( run_load )); then
    compose+=(-f "$repo_root/backend/load_test/compose.yml")
    image_tags+=("$image_prefix-fixture")
    if [[ "${LOAD_DATABASE_STORAGE:-disk}" == memory ]]; then
        compose+=(-f "$repo_root/backend/load_test/compose.memory.yml")
    fi
    if [[ "${LOAD_CONTAINER_LOGGING:-default}" == local ]]; then
        compose+=(-f "$repo_root/backend/load_test/compose.logging.yml")
    fi
fi
compose_started=0
image_cleanup=0
if (( run_monitoring )); then
    compose+=(-f "$test_root/deployment/docker/compose.monitoring.yml" --profile monitoring)
fi

cleanup() {
    result=$?
    trap - EXIT INT TERM
    if (( compose_started )); then
        if (( result != 0 )); then
            "${compose[@]}" logs --tail 40 initializer pgbouncer api receiver ticker worker rabbitmq >&2 || true
            if (( run_monitoring )); then
                "${compose[@]}" logs --tail 40 prometheus grafana >&2 || true
            fi
        fi
        "${compose[@]}" down --volumes --remove-orphans --timeout 15 >/dev/null 2>&1 || true
    fi
    if (( image_cleanup )); then
        for image in "${image_tags[@]}"; do
            docker image rm "$image" >/dev/null 2>&1 || true
        done
    fi
    case "$test_root" in
        "$tmp_parent"/forcad-deployment.*) rm -rf -- "$test_root" ;;
        *) echo 'Refusing to remove an unexpected temporary path.' >&2; result=1 ;;
    esac
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ -n "$(docker ps -aq --filter "label=com.docker.compose.project=$test_project")" || \
      -n "$(docker network ls -q --filter "label=com.docker.compose.project=$test_project")" || \
      -n "$(docker volume ls -q --filter "label=com.docker.compose.project=$test_project")" ]]; then
    echo 'Refusing to reuse an existing deployment test project.' >&2
    exit 2
fi
for image in "${image_tags[@]}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
        echo 'Refusing to overwrite an existing deployment test image.' >&2
        exit 2
    fi
done
image_cleanup=1

# Do not inherit project settings from the owner shell or a checkout-local .env.
unset FORCAD_NGINX_BIND PUBLIC_ORIGIN TRUSTED_PROXY_CIDRS CHECKER_JOB_GRACE \
    FORCAD_API_HTTP_MAX_INFLIGHT FORCAD_RECEIVER_HTTP_MAX_INFLIGHT \
    DATABASE_MAX_CONNS FORCAD_POSTGRES_MAX_CONNECTIONS \
    FORCAD_API_DATABASE_MAX_CONNS FORCAD_RECEIVER_DATABASE_MAX_CONNS \
    FORCAD_WORKER_DATABASE_MAX_CONNS FORCAD_TICKER_DATABASE_MAX_CONNS \
    PGBOUNCER_API_CONNECTIONS PGBOUNCER_API_MIN_CONNECTIONS PGBOUNCER_RECEIVER_CONNECTIONS \
    PGBOUNCER_WORKER_CONNECTIONS PGBOUNCER_TICKER_CONNECTIONS DATABASE_POOLER_ADDR \
    CHECKER_WORKFLOW_TIMEOUT FORCAD_WORKER_CONCURRENCY FORCAD_CHECKER_MEMORY_LIMIT_MB \
    FORCAD_CHECKER_OUTPUT_LIMIT_BYTES FLAG_RATE FLAG_BURST FORCAD_SERVICE_MEMORY \
    FORCAD_SERVICE_MEMORY_RESERVATION FORCAD_SERVICE_PIDS \
    FORCAD_RECEIVER_MEMORY FORCAD_RECEIVER_MEMORY_RESERVATION \
    FORCAD_WORKER_MEMORY FORCAD_WORKER_MEMORY_RESERVATION FORCAD_WORKER_PIDS PUBLIC_SCHEME \
    VITE_BRAND_NAME VITE_BRAND_LOGO VITE_BRAND_PRIMARY VITE_BRAND_ACCENT

install -d -m 0700 "$test_root/deployment/docker" "$test_root/config" \
    "$test_root/checkers" "$test_root/public/teams" "$test_root/bin" \
    "$test_root/artifacts"
install -m 0600 "$repo_root/deployment/docker/compose.yml" "$test_root/deployment/docker/compose.yml"
if (( run_monitoring )); then
    cp -R "$repo_root/deployment/monitoring" "$test_root/deployment/monitoring"
    # Provisioning contains no secrets; the non-root collectors must read it.
    chmod -R a+rX "$test_root/deployment/monitoring"
    install -m 0600 "$repo_root/deployment/docker/compose.monitoring.yml" "$test_root/deployment/docker/compose.monitoring.yml"
    export FORCAD_GRAFANA_PORT= FORCAD_PROMETHEUS_PORT=
    export GRAFANA_ADMIN_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
fi
install -m 0600 "$repo_root/backend/integration_test/fixtures/config.yml" "$test_root/config/config.yml"
if (( run_load )); then
    python3 "$repo_root/backend/load_test/scripts/prepare.py" "$test_root/config/config.yml"
    install -m 0755 "$repo_root/backend/load_test/test-data/checkers/"*.py "$test_root/checkers/"
fi
install -m 0755 "$repo_root/backend/integration_test/fixtures/fixture.py" "$test_root/checkers/fixture.py"
chmod 0755 "$test_root/checkers"

go -C "$repo_root/backend" build -o "$test_root/bin/forcad" ./cmd/forcad
"$test_root/bin/forcad" setup --project-root "$test_root"
{
    cat "$test_root/config/runtime/admin.env"
    printf '\n%s\n' \
        'FORCAD_E2E_TEAM_ONE_TOKEN=0000000000000001' \
        'FORCAD_E2E_TEAM_TWO_TOKEN=0000000000000002'
} > "$runtime_env"
chmod 0600 "$runtime_env"

export FORCAD_TEST_BUILD_ROOT="$repo_root"
export FORCAD_TEST_IMAGE_PREFIX="$image_prefix"
export FORCAD_TEST_PROJECT_ROOT="$test_root"
export FORCAD_E2E_PROJECT="$test_project"
export FORCAD_E2E_ADMIN_ENV_FILE="$runtime_env"
export FORCAD_E2E_RUNTIME_ENV_HOST="$runtime_env"
export FORCAD_E2E_FRONTEND_DIR="$repo_root/frontend"
export FORCAD_E2E_ARTIFACTS="$test_root/artifacts"
e2e_uid="$(id -u)"
e2e_gid="$(id -g)"
if (( run_e2e )); then
    docker_security_options="$(docker info --format '{{json .SecurityOptions}}')"
    case "$docker_security_options" in
        *rootless*)
            # Rootless container UID 0 maps to the invoking host user, which
            # owns the private runtime and artifact directories.
            e2e_uid=0
            e2e_gid=0
            ;;
    esac
fi
export FORCAD_E2E_UID="$e2e_uid"
export FORCAD_E2E_GID="$e2e_gid"

compose_started=1
if ! "${compose[@]}" up -d --build --wait --wait-timeout 180 \
    --scale "api=$api_replicas" --scale receiver=2 --scale worker=2; then
    "${compose[@]}" ps -a >&2
    for container in $("${compose[@]}" ps -aq pgbouncer initializer); do
        docker inspect "$container" --format '{{.Name}} {{.State.Error}} {{if .State.Health}}{{range .State.Health.Log}}{{.ExitCode}} {{.Output}}{{end}}{{end}}' >&2
        docker logs --tail 30 "$container" >&2
    done
    exit 1
fi

"${compose[@]}" exec -T worker python - < "$repo_root/backend/integration_test/scripts/check_worker.py"

if (( run_e2e )); then
    "${compose[@]}" --profile e2e run --rm --no-deps e2e
fi

http_address="$("${compose[@]}" port nginx 8080)"
if (( run_load )); then
    FORCAD_LOAD_ROOT="$test_root" FORCAD_LOAD_PROJECT="$test_project" \
        FORCAD_LOAD_ORIGIN="http://$http_address" \
        go -C "$repo_root/backend" test -count=1 -timeout=24h -run '^TestLoad$' -v ./load_test
    exit
fi
python3 "$repo_root/backend/integration_test/scripts/check_deployment.py" \
    "$test_root" "$test_project" "http://$http_address"
if (( run_monitoring )); then
    python3 "$repo_root/backend/integration_test/scripts/check_monitoring.py" \
        "http://$("${compose[@]}" port prometheus 9090)" \
        "http://$("${compose[@]}" port grafana 3000)" "$repo_root" "$test_project"
fi

#!/usr/bin/env bash
set -euo pipefail
umask 077
run_load=0
run_monitoring=0
api_replicas=2
windows_host=0
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) windows_host=1 ;;
esac

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
    case "${LOAD_MONITORING:-0}" in
        0) ;;
        1) run_monitoring=1 ;;
        *) echo 'LOAD_MONITORING must be 0 or 1.' >&2; exit 2 ;;
    esac
    case "${LOAD_CAPACITY:-}" in
        ''|chrp-final) ;;
        *) echo 'LOAD_CAPACITY must be chrp-final or empty.' >&2; exit 2 ;;
    esac
    case "${LOAD_CLIENT_BUCKETS:-0}" in
        0|1) ;;
        *) echo 'LOAD_CLIENT_BUCKETS must be 0 or 1.' >&2; exit 2 ;;
    esac
    case "${LOAD_CHECKER_AUDIT:-0}" in
        0|1) ;;
        *) echo 'LOAD_CHECKER_AUDIT must be 0 or 1.' >&2; exit 2 ;;
    esac
    receiver_replicas="${LOAD_RECEIVER_REPLICAS:-2}"
    if [[ ! "$receiver_replicas" =~ ^([1-9]|1[0-6])$ ]]; then
        echo 'LOAD_RECEIVER_REPLICAS must be an integer between 1 and 16.' >&2
        exit 2
    fi
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
    case "${LOAD_BROKER_PREFLIGHT:-0}" in
        0|1) ;;
        *) echo 'LOAD_BROKER_PREFLIGHT must be 0 or 1.' >&2; exit 2 ;;
    esac
    worker_replicas="${LOAD_WORKER_REPLICAS:-2}"
    if [[ ! "$worker_replicas" =~ ^([1-9]|1[0-9]|20)$ ]]; then
        echo 'LOAD_WORKER_REPLICAS must be an integer between 1 and 20.' >&2
        exit 2
    fi
    team_count="${LOAD_TEAM_COUNT:-20}"
    if [[ ! "$team_count" =~ ^([2-9]|1[0-9]|2[0-8])$ ]]; then
        echo 'LOAD_TEAM_COUNT must be an integer between 2 and 28.' >&2
        exit 2
    fi
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
tmp_parent="${TMPDIR:-/tmp}"
tmp_parent="$(cd "$tmp_parent" && pwd -P)"
test_root="$(mktemp -d "$tmp_parent/forcad-deployment.XXXXXX")"
if (( windows_host )); then
    # MSYS chmod/install modes do not secure NTFS files. Give this fresh tree
    # a private, inheritable DACL before writing configuration or credentials.
    private_acl_script="$(cat <<'PS'
& {
$ErrorActionPreference = 'Stop'
try {
$directory = Get-Item -LiteralPath $env:FORCAD_TEST_PRIVATE_ROOT
if (-not $directory.PSIsContainer -or ($directory.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
    throw 'Expected an ordinary temporary directory'
}
$owner = [Security.Principal.WindowsIdentity]::GetCurrent().User
$allowed = @($owner.Value, 'S-1-5-18', 'S-1-5-32-544')
$acl = New-Object Security.AccessControl.DirectorySecurity
$acl.SetOwner($owner)
$acl.SetAccessRuleProtection($true, $false)
foreach ($sid in $allowed) {
    $identity = New-Object Security.Principal.SecurityIdentifier($sid)
    $rule = New-Object Security.AccessControl.FileSystemAccessRule($identity, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
    $acl.AddAccessRule($rule)
}
Set-Acl -LiteralPath $directory.FullName -AclObject $acl
$verified = Get-Acl -LiteralPath $directory.FullName
if (-not $verified.AreAccessRulesProtected) { throw 'Temporary directory inherited public permissions' }
$rules = @($verified.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier]))
if ($rules.Count -ne $allowed.Count) { throw 'Unexpected temporary directory access rule count' }
foreach ($rule in $rules) {
    if ($rule.IdentityReference.Value -notin $allowed -or $rule.AccessControlType -ne 'Allow' -or
        $rule.FileSystemRights -ne 'FullControl' -or $rule.InheritanceFlags -ne 'ContainerInherit,ObjectInherit') {
        throw 'Unexpected temporary directory access rule'
    }
}
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
}

PS
)"
    FORCAD_TEST_PRIVATE_ROOT="$(cygpath -w "$test_root")" powershell.exe -NoProfile -NonInteractive -Command "$private_acl_script"
    unset private_acl_script
    # Native copies inherit the verified DACL; GNU install's POSIX fchmod fails
    # on this filesystem. Container-side staging applies Linux owner/mode bits.
    install() {
        python3 - "$@" <<'PY'
from pathlib import Path
import shutil
import sys

args = sys.argv[1:]
directory = args[:1] == ["-d"]
if directory:
    args = args[1:]
if len(args) < 3 or args[0] != "-m" or args[1] not in {"0600", "0644", "0700", "0755"}:
    raise SystemExit("Unsupported private Windows install arguments")
paths = [Path(value) for value in args[2:]]
if directory:
    for path in paths:
        path.mkdir(parents=True, exist_ok=True)
else:
    if len(paths) < 2 or (len(paths) > 2 and not paths[-1].is_dir()):
        raise SystemExit("Expected source files and one destination")
    for source in paths[:-1]:
        destination = paths[-1] / source.name if paths[-1].is_dir() else paths[-1]
        shutil.copyfile(source, destination)
PY
    }
else
    chmod 0700 "$test_root"
fi
test_project="forcad-deployment-$(id -u)-$$-$RANDOM"
if (( run_load )) && [[ -n "${LOAD_REPORT_DIR:-}" ]]; then
    mkdir -p "$LOAD_REPORT_DIR"
    printf '%s\n' "$test_project" > "$LOAD_REPORT_DIR/compose-project.txt"
fi
image_prefix="$test_project"
runtime_env="$test_root/runtime.env"
compose_env="$test_root/compose.env"
: > "$compose_env"
compose=(docker compose --env-file "$compose_env" --project-name "$test_project" --project-directory "$test_root/deployment/docker" -f "$test_root/deployment/docker/compose.yml" -f "$repo_root/deployment/docker/compose.e2e.yml")
image_tags=("$image_prefix-runtime" "$image_prefix-worker" "$image_prefix-nginx" "$image_prefix-postgres" "$image_prefix-pgbouncer" "$image_prefix-nats")
if (( run_load )); then
    compose+=(-f "$repo_root/backend/load_test/compose.yml")
    if [[ "${LOAD_CAPACITY:-}" == chrp-final ]]; then
        compose+=(-f "$repo_root/backend/load_test/compose.final.yml")
    fi
    if [[ "${LOAD_CLIENT_BUCKETS:-0}" == 1 ]]; then
        compose+=(-f "$repo_root/backend/load_test/compose.clients.yml")
    fi
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
            "${compose[@]}" logs --tail 40 initializer pgbouncer api receiver ticker worker nats >&2 || true
            if (( run_monitoring )); then
                "${compose[@]}" logs --tail 40 prometheus grafana checker-db-access >&2 || true
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
unset COMPOSE_PROFILES COMPOSE_ENV_FILES COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_DISABLE_ENV_FILE \
    FORCAD_NGINX_BIND PUBLIC_ORIGIN TRUSTED_PROXY_CIDRS CHECKER_JOB_GRACE \
    FORCAD_API_HTTP_MAX_INFLIGHT FORCAD_RECEIVER_HTTP_MAX_INFLIGHT \
    DATABASE_MAX_CONNS DATABASE_MIN_CONNS DATABASE_TIMEOUT FORCAD_POSTGRES_MAX_CONNECTIONS \
    FORCAD_API_DATABASE_MAX_CONNS FORCAD_RECEIVER_DATABASE_MAX_CONNS \
    FORCAD_WORKER_DATABASE_MAX_CONNS FORCAD_TICKER_DATABASE_MAX_CONNS \
    PGBOUNCER_API_CONNECTIONS PGBOUNCER_API_MIN_CONNECTIONS PGBOUNCER_RECEIVER_CONNECTIONS \
    PGBOUNCER_WORKER_CONNECTIONS PGBOUNCER_TICKER_CONNECTIONS DATABASE_POOLER_ADDR \
    PGBOUNCER_MAX_CLIENTS PGBOUNCER_API_CLIENTS PGBOUNCER_RECEIVER_CLIENTS \
    PGBOUNCER_WORKER_CLIENTS PGBOUNCER_TICKER_CLIENTS \
    FORCAD_RUNTIME_DB_USER FORCAD_RUNTIME_DB_PASSWORD FORCAD_REQUIRE_RUNTIME_DB \
    FORCAD_DATABASE_REQUIRE_TLS FORCAD_DB_TLS_DIR POSTGRES_SSLMODE POSTGRES_SSLROOTCERT \
    PGBOUNCER_CLIENT_TLS_SSLMODE PGBOUNCER_CLIENT_TLS_CERT_FILE PGBOUNCER_CLIENT_TLS_KEY_FILE \
    PGBOUNCER_SERVER_TLS_SSLMODE PGBOUNCER_SERVER_TLS_CA_FILE PGSSLMODE PGSSLROOTCERT \
    CHECKER_WORKFLOW_TIMEOUT FORCAD_WORKER_CONCURRENCY FORCAD_CHECKER_MEMORY_LIMIT_MB \
    FORCAD_CHECKER_OUTPUT_LIMIT_BYTES FLAG_RATE FLAG_BURST FORCAD_SERVICE_MEMORY \
    FORCAD_SERVICE_MEMORY_RESERVATION FORCAD_SERVICE_PIDS \
    FORCAD_RECEIVER_MEMORY FORCAD_RECEIVER_MEMORY_RESERVATION \
    FORCAD_WORKER_MEMORY FORCAD_WORKER_MEMORY_RESERVATION FORCAD_WORKER_PIDS PUBLIC_SCHEME \
    FORCAD_API_MEMORY FORCAD_API_MEMORY_RESERVATION FORCAD_API_DATABASE_MIN_CONNS \
    FORCAD_API_GOGC FORCAD_API_GOMEMLIMIT FORCAD_RECEIVER_GOMEMLIMIT FORCAD_WORKER_GOMEMLIMIT \
    FORCAD_TICKER_MEMORY FORCAD_TICKER_MEMORY_RESERVATION \
    FORCAD_REDIS_MEMORY FORCAD_REDIS_MEMORY_RESERVATION \
    FORCAD_NATS_MEMORY FORCAD_NATS_MEMORY_RESERVATION \
    FORCAD_PGBOUNCER_MEMORY FORCAD_PGBOUNCER_MEMORY_RESERVATION \
    FORCAD_POSTGRES_MEMORY FORCAD_POSTGRES_MEMORY_RESERVATION \
    FORCAD_POSTGRES_SHM_SIZE FORCAD_POSTGRES_SHARED_BUFFERS \
    FORCAD_PROMETHEUS_MEMORY FORCAD_PROMETHEUS_MEMORY_RESERVATION \
    FORCAD_PROMETHEUS_RETENTION_TIME FORCAD_PROMETHEUS_RETENTION_SIZE \
    FORCAD_PROMETHEUS_QUERY_CONCURRENCY FORCAD_PROMETHEUS_QUERY_SAMPLES \
    FORCAD_GRAFANA_MEMORY FORCAD_GRAFANA_MEMORY_RESERVATION \
    FORCAD_API_REPLICAS FORCAD_RECEIVER_REPLICAS FORCAD_WORKER_REPLICAS FORCAD_TICKER_REPLICAS \
    VITE_BRAND_NAME VITE_BRAND_LOGO VITE_BRAND_PRIMARY VITE_BRAND_ACCENT \
    FORCAD_POSTGRES_IMAGE NATS_URL NATS_USER NATS_PASSWORD NATS_STREAM NATS_SUBJECT \
    NATS_CONSUMER NATS_MAX_ACK_PENDING

# PgBouncer's client admission budget must describe the same replica counts as
# the explicit --scale arguments below, including the checker load profile.
if (( run_load )) && [[ "${LOAD_CAPACITY:-}" == chrp-final ]]; then
    install -m 0600 "$repo_root/deployment/final/limits.env.example" "$compose_env"
    while IFS='=' read -r key _; do
        [[ "$key" =~ ^[A-Z][A-Z0-9_]*$ ]] || continue
        unset "$key"
    done < "$compose_env"
fi
if (( run_load )) && [[ "${LOAD_CAPACITY:-}" == chrp-final || "${LOAD_CLIENT_BUCKETS:-0}" == 1 ]]; then
    install -d -m 0755 "$test_root/deployment/nginx"
    python3 - "$repo_root" "$test_root" <<'PY'
from pathlib import Path
import sys

repo, target = map(Path, sys.argv[1:])
config = (repo / 'deployment/nginx/http.conf.template').read_text()
original = 'limit_req_zone $binary_remote_addr zone=api_per_ip:10m rate=150r/s;'
if config.count(original) != 1:
    raise SystemExit('Expected one production REST limiter; inspect the load test adapter')
# The isolated, loopback-only generator represents individual clients. Keep
# the same per-client rate, burst and rejection code as production.
replacement = '''map $http_x_forcad_load_client $load_client_key {
    default $binary_remote_addr;
    "~^client-[0-9]{1,4}$" $http_x_forcad_load_client;
}
limit_req_zone $load_client_key zone=api_per_ip:10m rate=150r/s;'''
config = config.replace(original, replacement).replace('${DNS_RESOLVER}', '127.0.0.11')
path = target / 'deployment/nginx/http.conf'
path.write_text(config)
path.chmod(0o644)
PY
fi
export FORCAD_API_REPLICAS="$api_replicas"
export FORCAD_RECEIVER_REPLICAS="${receiver_replicas:-2}"
export FORCAD_WORKER_REPLICAS="${worker_replicas:-2}"
export FORCAD_TICKER_REPLICAS=1

install -d -m 0700 "$test_root/deployment/docker" "$test_root/config" \
    "$test_root/checkers" "$test_root/public/teams" "$test_root/bin" \
    "$test_root/artifacts"
install -m 0600 "$repo_root/deployment/docker/compose.yml" "$test_root/deployment/docker/compose.yml"
if (( run_load && windows_host )) && [[ "${LOAD_BROKER_PREFLIGHT:-0}" == 1 ]]; then
    # Docker Desktop's bridge addresses are not reachable from native Windows.
    # Keep the published endpoint stable when the reconnect test restarts NATS.
    nats_preflight_port="$(python3 - <<'PY'
import socket
with socket.socket() as listener:
    listener.bind(('127.0.0.1', 0))
    print(listener.getsockname()[1])
PY
)"
    cat > "$test_root/deployment/docker/compose.broker-preflight.yml" <<YAML
services:
  nats:
    ports: !override ["127.0.0.1:${nats_preflight_port}:4222"]
YAML
    compose+=(-f "$test_root/deployment/docker/compose.broker-preflight.yml")
fi
if (( run_monitoring )); then
    cp -R "$repo_root/deployment/monitoring" "$test_root/deployment/monitoring"
    # Provisioning contains no secrets; the non-root collectors must read it.
    chmod -R a+rX "$test_root/deployment/monitoring"
    install -m 0600 "$repo_root/deployment/docker/compose.monitoring.yml" "$test_root/deployment/docker/compose.monitoring.yml"
    install -d -m 0755 "$test_root/backend/db/migrations"
    install -m 0644 "$repo_root/backend/db/migrations/00001_initial_schema.sql" \
        "$test_root/backend/db/migrations/00001_initial_schema.sql"
    export FORCAD_GRAFANA_PORT='' FORCAD_PROMETHEUS_PORT=''
    export FORCAD_POSTGRES_IMAGE="$image_prefix-postgres"
    GRAFANA_ADMIN_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
    GRAFANA_CHECKER_DB_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
    export GRAFANA_ADMIN_PASSWORD GRAFANA_CHECKER_DB_PASSWORD
fi
install -m 0600 "$repo_root/backend/integration_test/fixtures/config.yml" "$test_root/config/config.yml"
if (( run_load )); then
    python3 "$repo_root/backend/load_test/scripts/prepare.py" "$test_root/config/config.yml"
    install -m 0755 "$repo_root/backend/load_test/test-data/checkers/"*.py "$test_root/checkers/"
fi
install -m 0755 "$repo_root/backend/integration_test/fixtures/fixture.py" "$test_root/checkers/fixture.py"
if (( !windows_host )); then chmod 0755 "$test_root/checkers"; fi

go -C "$repo_root/backend" build -o "$test_root/bin/forcad" ./cmd/forcad
"$test_root/bin/forcad" setup --project-root "$test_root"
{
    cat "$test_root/config/runtime/admin.env"
    printf '\n%s\n' \
        'FORCAD_E2E_TEAM_ONE_TOKEN=0000000000000001' \
        'FORCAD_E2E_TEAM_TWO_TOKEN=0000000000000002'
} > "$runtime_env"
if (( !windows_host )); then chmod 0600 "$runtime_env"; fi

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
    --scale "api=$api_replicas" --scale "receiver=${receiver_replicas:-2}" --scale "worker=${worker_replicas:-2}"; then
    "${compose[@]}" ps -a >&2
    for container in $("${compose[@]}" ps -aq pgbouncer initializer); do
        docker inspect "$container" --format '{{.Name}} {{.State.Error}} {{if .State.Health}}{{range .State.Health.Log}}{{.ExitCode}} {{.Output}}{{end}}{{end}}' >&2
        docker logs --tail 30 "$container" >&2
    done
    exit 1
fi
if (( run_load )); then
    running_workers="$("${compose[@]}" ps --status running -q worker | wc -l)"
    if (( running_workers != worker_replicas )); then
        echo "Expected $worker_replicas running workers, found $running_workers." >&2
        exit 1
    fi
    echo "Healthy worker replicas: $running_workers"
fi

"${compose[@]}" exec -T worker python - < "$repo_root/backend/integration_test/scripts/check_worker.py"

if (( run_e2e )); then
    "${compose[@]}" --profile e2e run --rm --no-deps e2e
fi

http_address="$("${compose[@]}" port nginx 8080)"
if (( run_load && run_monitoring )); then
    echo "Prometheus: http://$("${compose[@]}" port prometheus 9090)"
    echo "Grafana: http://$("${compose[@]}" port grafana 3000)"
fi
if (( run_load )); then
    if [[ "${LOAD_BROKER_PREFLIGHT:-0}" == 1 ]]; then
        nats_container="$("${compose[@]}" ps --status running -q nats)"
        nats_client_address=
        if (( windows_host )); then
            nats_client_address="$("${compose[@]}" port nats 4222)"
        fi
        python3 - "$repo_root/backend" "$test_root" "$test_project" "$nats_container" \
            "$nats_client_address" <<'PY'
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import quote, unquote, urlsplit

backend, root, project, container, published_client = sys.argv[1:]
secrets = []


def redact(value):
    for secret in sorted(secrets, key=len, reverse=True):
        if secret:
            value = value.replace(secret, "[redacted]")
    return value


def broker_address(item):
    if published_client:
        ports = item.get("NetworkSettings", {}).get("Ports", {})
        match = re.fullmatch(r"127\.0\.0\.1:([1-9][0-9]{0,4})", published_client)
        if not match or int(match[1]) > 65535:
            raise RuntimeError("expected one loopback-only published NATS endpoint")
        if ports.get("4222/tcp") != [{"HostIp": "127.0.0.1", "HostPort": match[1]}]:
            raise RuntimeError("published NATS endpoint did not match the isolated container")
        return published_client
    addresses = {str(ipaddress.IPv4Address(network["IPAddress"]))
                 for network in item.get("NetworkSettings", {}).get("Networks", {}).values()
                 if network.get("IPAddress")}
    if len(addresses) != 1:
        raise RuntimeError("expected one directly reachable NATS container IPv4 address")
    host = addresses.pop()
    return f"{host}:4222"


def preflight():
    if not re.fullmatch(r"forcad-deployment-[a-z0-9-]+", project) or not re.fullmatch(r"[a-f0-9]{64}", container):
        raise RuntimeError("expected one NATS container from this isolated Compose project")
    inspected = subprocess.run(["docker", "inspect", "--type", "container", container],
                               check=True, capture_output=True, text=True, timeout=15)
    items = json.loads(inspected.stdout)
    if len(items) != 1:
        raise RuntimeError("ambiguous NATS container inventory")
    item = items[0]
    labels = item.get("Config", {}).get("Labels", {})
    if (item.get("Id") != container or labels.get("com.docker.compose.project") != project
            or labels.get("com.docker.compose.service") != "nats" or not item.get("State", {}).get("Running")):
        raise RuntimeError("NATS container identity or running state did not match")
    endpoint = broker_address(item)
    environment = dict(line.split("=", 1) for line in
                       (Path(root) / "config/runtime/nats_environment.env").read_text().splitlines()
                       if line and not line.startswith("#") and "=" in line)
    actual_environment = dict(entry.split("=", 1) for entry in item["Config"].get("Env", []) if "=" in entry)
    for key in ("NATS_USER", "NATS_PASSWORD"):
        if not environment.get(key) or actual_environment.get(key) != environment[key]:
            raise RuntimeError("NATS runtime credentials did not match the isolated container")
    username, password = environment["NATS_USER"], environment["NATS_PASSWORD"]
    runtime_url = urlsplit(environment.get("NATS_URL", ""))
    if (runtime_url.scheme != "nats" or runtime_url.hostname != "nats" or runtime_url.port != 4222
            or unquote(runtime_url.username or "") != username or unquote(runtime_url.password or "") != password
            or runtime_url.path or runtime_url.query or runtime_url.fragment):
        raise RuntimeError("NATS runtime URL did not match the isolated service and credentials")
    address = f"nats://{quote(username, safe='')}:{quote(password, safe='')}@{endpoint}"
    secrets.extend([password, quote(password, safe=""), environment.get("NATS_URL", ""), address])
    prefix = "forcad_e2e_" + project.replace("-", "_")
    child_environment = os.environ.copy()
    child_environment.update(FORCAD_TEST_NATS_URL=address, FORCAD_TEST_NATS_PREFIX=prefix,
                             FORCAD_TEST_NATS_CONTAINER=container, FORCAD_TEST_NATS_PROJECT=project)
    required = {"TestDrainFinishesCurrentDeliveryAndLeavesQueuedWork",
                "TestConsumerReconnectsAndResumesDelivery"}
    def save_log(raw):
        output = redact(raw)
        if os.environ.get("LOAD_REPORT_DIR"):
            log_path = Path(os.environ["LOAD_REPORT_DIR"]) / "broker-preflight.jsonl"
            log_path.write_text(output, encoding="utf-8")
            log_path.chmod(0o600)
        return output

    try:
        result = subprocess.run(["go", "test", "-count=1", "-timeout=2m", "-json", "-run",
                                 "^(" + "|".join(sorted(required)) + ")$", "./integration_test/nats"],
                                cwd=backend, env=child_environment, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, text=True, timeout=150)
    except subprocess.TimeoutExpired as error:
        raw = error.stdout or ""
        if isinstance(raw, bytes):
            raw = raw.decode(errors="replace")
        print(save_log(raw), end="", flush=True)
        raise RuntimeError("NATS drain/reconnect preflight timed out") from None
    output = save_log(result.stdout)
    passed = set()
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            print(redact(line), flush=True)
            continue
        if event.get("Output"):
            print(redact(event["Output"]), end="", flush=True)
        if event.get("Action") == "pass" and event.get("Test") in required:
            passed.add(event["Test"])
    if result.returncode != 0 or passed != required:
        raise RuntimeError("NATS drain/reconnect preflight did not pass both required tests")
    print("NATS JetStream preflight passed: drain and broker-restart reconnect on the load stack.", flush=True)


try:
    preflight()
except Exception as error:
    raise SystemExit("NATS preflight failed: " + redact(str(error))) from None
PY
    fi
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

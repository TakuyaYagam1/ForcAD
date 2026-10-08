#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image_tag="forcad-nginx:integration-$$"
container_name="forcad-nginx-check-$$"
container_id=""

cleanup() {
    if [[ -n "$container_id" ]]; then
        docker rm --force "$container_id" >/dev/null 2>&1 || true
    fi
    docker image rm "$image_tag" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker build \
    --build-arg DNS_RESOLVER=127.0.0.11 \
    --file "$repo_root/docker_config/nginx/Dockerfile" \
    --tag "$image_tag" \
    "$repo_root"

docker run --rm --entrypoint sh "$image_tag" -ec '
    id
    ls -ld /tmp /tmp/nginx
    ls -la /tmp/nginx
    touch /tmp/nginx/.permission-check
    rm /tmp/nginx/.permission-check
'

host_port="$(python3 - <<'PY'
import socket

with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"
container_id="$(docker run --detach \
    --name "$container_name" \
    --publish "127.0.0.1:${host_port}:8080" \
    --env PUBLIC_SCHEME=https \
    --env TRUSTED_PROXY_CIDRS=127.0.0.1/32 \
    "$image_tag")"

container_state="$(docker inspect --format '{{.State.Status}}' "$container_id")"
if [[ "$container_state" != running ]]; then
    docker logs "$container_id" >&2
    echo "nginx container exited during startup." >&2
    exit 1
fi

if [[ "$(docker inspect --format '{{.Config.User}}' "$container_id")" != nginx ]]; then
    echo "nginx image must run as the nginx user." >&2
    exit 1
fi
docker exec "$container_id" sh -ec '
    test "$(id -u)" -ne 0
    test -w /tmp/forcad-proxy-proto
    test -w /tmp/forcad-trusted-proxies.conf
    grep -Fx "proxy_set_header X-Forwarded-Proto https;" /tmp/forcad-proxy-proto
    grep -Fx "set_real_ip_from 127.0.0.1/32;" /tmp/forcad-trusted-proxies.conf
'

base_url="http://127.0.0.1:${host_port}"
ready=0
for _ in $(seq 1 40); do
    if curl --fail --silent --max-time 2 "$base_url/" >/dev/null; then
        ready=1
        break
    fi
    sleep 0.5
done
if [[ "$ready" != 1 ]]; then
    docker logs "$container_id" >&2
    echo "nginx did not become ready." >&2
    exit 1
fi

while IFS= read -r logo_path; do
    [[ "$logo_path" == /team-logos/* ]] || {
        echo "Unexpected mapped team logo URL: $logo_path" >&2
        exit 1
    }
    curl --fail --silent --show-error --max-time 5 \
        "$base_url$logo_path" >/dev/null
done < <(PYTHONPATH="$repo_root/backend/lib" python -c \
    'from team_logos import TEAM_LOGOS; print("\n".join(sorted(set(TEAM_LOGOS.values()))))')

echo "nginx served every mapped team logo URL."

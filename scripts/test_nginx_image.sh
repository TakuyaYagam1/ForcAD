#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/forcad-nginx.XXXXXX")"
run_suffix="${test_dir##*.}"
image_tag="forcad-nginx:integration-${run_suffix}"
container_name="forcad-nginx-check-${run_suffix}"
container_id=""
image_created=0
logo_fixture_dir="$test_dir/team-logos"

cleanup() {
    if [[ -n "$container_id" ]]; then
        docker rm --force "$container_id" >/dev/null 2>&1 || true
    fi
    if [[ "$image_created" == 1 ]]; then
        docker image rm "$image_tag" >/dev/null 2>&1 || true
    fi
    rm -rf -- "$test_dir" || true
}
trap cleanup EXIT

if docker image inspect "$image_tag" >/dev/null 2>&1; then
    echo "Refusing to overwrite existing Docker image: $image_tag" >&2
    exit 2
fi
if docker container inspect "$container_name" >/dev/null 2>&1; then
    echo "Refusing to overwrite existing Docker container: $container_name" >&2
    exit 2
fi

mkdir -p "$logo_fixture_dir"
chmod 0755 "$logo_fixture_dir"
printf '<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>\n' \
    > "$logo_fixture_dir/fixture.svg"
chmod 0644 "$logo_fixture_dir/fixture.svg"

docker build \
    --build-arg DNS_RESOLVER=127.0.0.11 \
    --build-arg VITE_BRAND_NAME=ForcADThemeSmoke \
    --build-arg VITE_BRAND_LOGO=/brand/forcad-mark.svg \
    --build-arg VITE_BRAND_PRIMARY='#5B8DEF' \
    --build-arg VITE_BRAND_ACCENT='#38B7B0' \
    --file "$repo_root/frontend/Dockerfile" \
    --tag "$image_tag" \
    "$repo_root"
image_created=1

docker run --rm --entrypoint sh "$image_tag" -ec \
    'grep -R -Fq "ForcADThemeSmoke" /var/www/front'

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
    --mount "type=bind,src=$logo_fixture_dir,dst=/var/www/front/team-logos,readonly" \
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

curl --fail --silent --show-error --max-time 5 \
    "$base_url/team-logos/fixture.svg" \
    --dump-header "$test_dir/logo.headers" \
    --output "$test_dir/response.svg"
cmp "$logo_fixture_dir/fixture.svg" "$test_dir/response.svg"
grep -Eiq '^X-Content-Type-Options: nosniff' "$test_dir/logo.headers"
grep -Eiq '^Content-Security-Policy:.*sandbox' "$test_dir/logo.headers"

curl --fail --silent --show-error --max-time 5 \
    "$base_url/" --output "$test_dir/index.html"
curl --fail --silent --show-error --max-time 5 \
    "$base_url/teams" --output "$test_dir/teams.html"
cmp "$test_dir/index.html" "$test_dir/teams.html"

for route in /teams/fixture.svg /%74eams/fixture.svg; do
    status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
        --max-time 5 "$base_url$route")"
    if [[ "$status" != 404 ]]; then
        echo "Expected $route to return 404, got $status." >&2
        exit 1
    fi
done

missing_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
    --max-time 5 "$base_url/team-logos/missing.svg")"
if [[ "$missing_status" != 404 ]]; then
    echo "Expected a missing team logo to return 404, got $missing_status." >&2
    exit 1
fi

echo "nginx verified team logos, asset headers, the teams page, and build-time branding."

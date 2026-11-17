#!/usr/bin/env bash
set -euo pipefail

image="${1:-${IMAGE:-forcad-postgres:test}}"
if [[ "$#" -gt 1 ]]; then
    echo "Usage: $0 [postgres-image]" >&2
    exit 2
fi

test_dir="$(mktemp -d "${TMPDIR:-/tmp}/forcad-postgres.XXXXXX")"
run_suffix="${test_dir##*.}"
container_id=""
container_names=()

cleanup() {
    local container_name
    for container_name in "${container_names[@]}"; do
        docker rm --force "$container_name" >/dev/null 2>&1 || true
    done
    rm -rf -- "$test_dir" || true
}
trap cleanup EXIT

fail() {
    echo "$*" >&2
    exit 1
}

docker image inspect "$image" >/dev/null 2>&1 || fail "Docker image not found: $image"

start_case() {
    local case_name=$1
    local container_name="forcad-postgres-${case_name}-${run_suffix}"

    docker container inspect "$container_name" >/dev/null 2>&1 \
        && fail "Refusing to overwrite existing Docker container: $container_name"
    container_names+=("$container_name")
    docker run --detach --rm \
        --name "$container_name" \
        --network none \
        --read-only \
        --cap-drop ALL \
        --cap-add CHOWN \
        --cap-add DAC_OVERRIDE \
        --cap-add FOWNER \
        --security-opt no-new-privileges:true \
        --user 0:0 \
        --tmpfs /var/lib/postgresql:rw,mode=0700,uid=0,gid=0,size=256m \
        --tmpfs /tmp:rw,nosuid,nodev,mode=1777,size=32m \
        --entrypoint /bin/sh \
        "$image" -c 'while :; do sleep 60; done' >/dev/null
    container_id=$container_name

    [[ "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$container_id")" == true ]] || fail "Root filesystem must be read-only."
    [[ "$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$container_id")" == none ]] || fail "Test network must be disabled."
    [[ "$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$container_id")" == '["ALL"]' ]] || fail "All capabilities must be dropped."
    local actual_caps
    local expected_caps
    actual_caps="$(docker inspect --format '{{json .HostConfig.CapAdd}}' "$container_id" | tr -d '[]"' | tr ',' '\n' | sed 's/^CAP_//' | LC_ALL=C sort)"
    expected_caps="$(printf '%s\n' CHOWN DAC_OVERRIDE FOWNER | LC_ALL=C sort)"
    [[ "$actual_caps" == "$expected_caps" ]] || fail "Capabilities differ from postgres-data Compose settings."
    [[ "$(docker inspect --format '{{json .HostConfig.SecurityOpt}}' "$container_id")" == *'"no-new-privileges:true"'* ]] || fail "no-new-privileges must be enabled."
}

finish_case() {
    docker rm --force "$container_id" >/dev/null
    container_id=""
}

expect_refusal() {
    local expected=$1
    local command=$2
    local result
    if ! result="$(docker exec "$container_id" /bin/sh -ec "$command")"; then
        fail "The $expected refusal fixture failed its assertions."
    fi
    [[ "$result" == *"$expected"* ]] || fail "Refusal did not identify $expected."
}

echo "Testing PostgreSQL image: $image"

start_case fresh
pg_uid="$(docker exec "$container_id" id -u postgres)"
pg_gid="$(docker exec "$container_id" id -g postgres)"
docker exec "$container_id" /usr/local/bin/prepare-data
docker exec --user "$pg_uid:$pg_gid" "$container_id" /bin/sh -ec '
    test "$(stat -c %u /var/lib/postgresql)" = "$(id -u)"
    test "$(stat -c %a /var/lib/postgresql)" = 700
    test "$(stat -c %u /var/lib/postgresql/18)" = "$(id -u)"
    test "$(stat -c %a /var/lib/postgresql/18)" = 700
    test "$(stat -c %u /var/lib/postgresql/18/docker)" = "$(id -u)"
    test "$(stat -c %a /var/lib/postgresql/18/docker)" = 700
    : > /var/lib/postgresql/18/docker/.access-check
    rm /var/lib/postgresql/18/docker/.access-check
'
echo "PASS: fresh root-owned mode-0700 tmpfs is accessible by postgres."

docker exec --detach --user "$pg_uid:$pg_gid" \
    --env PGDATA=/var/lib/postgresql/18/docker \
    --env POSTGRES_HOST_AUTH_METHOD=trust \
    "$container_id" /usr/local/bin/docker-entrypoint.sh postgres \
    -c listen_addresses= -c unix_socket_directories=/tmp
ready=0
for _ in $(seq 1 40); do
    if docker exec "$container_id" pg_isready --host=/tmp >/dev/null 2>&1; then
        ready=1
        break
    fi
    sleep 0.5
done
[[ "$ready" == 1 ]] || fail "PostgreSQL did not start as nonroot."
docker exec "$container_id" /bin/sh -ec \
    "test \"\$(stat -c %u /var/lib/postgresql/18/docker/postmaster.pid)\" = \"$pg_uid\""
docker exec --user "$pg_uid:$pg_gid" "$container_id" \
    pg_ctl -D /var/lib/postgresql/18/docker -m fast -w stop >/dev/null
echo "PASS: PostgreSQL starts as nonroot on the adopted data root."

before_state="$(docker exec "$container_id" /bin/sh -ec '
    stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql /var/lib/postgresql/18 /var/lib/postgresql/18/docker /var/lib/postgresql/18/docker/PG_VERSION /var/lib/postgresql/18/docker/global/pg_control
    sha256sum /var/lib/postgresql/18/docker/PG_VERSION /var/lib/postgresql/18/docker/global/pg_control
')"
docker exec "$container_id" /usr/local/bin/prepare-data
after_state="$(docker exec "$container_id" /bin/sh -ec '
    stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql /var/lib/postgresql/18 /var/lib/postgresql/18/docker /var/lib/postgresql/18/docker/PG_VERSION /var/lib/postgresql/18/docker/global/pg_control
    sha256sum /var/lib/postgresql/18/docker/PG_VERSION /var/lib/postgresql/18/docker/global/pg_control
')"
[[ "$before_state" == "$after_state" ]] || fail "Valid PostgreSQL 18 data changed during prepare-data."
echo "PASS: existing valid PostgreSQL 18 cluster remains unchanged."
finish_case

start_case legacy
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "README.md" '
    printf "17\n" > /var/lib/postgresql/PG_VERSION
    chmod 0600 /var/lib/postgresql/PG_VERSION
    before_root=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql)
    before_marker=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql/PG_VERSION)
    before_hash=$(sha256sum /var/lib/postgresql/PG_VERSION)
    output=$(/usr/local/bin/prepare-data 2>&1) && exit 1
    test "$before_root" = "$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql)"
    test "$before_marker" = "$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql/PG_VERSION)"
    test "$before_hash" = "$(sha256sum /var/lib/postgresql/PG_VERSION)"
    test "$(cat /var/lib/postgresql/PG_VERSION)" = 17
    test ! -e /var/lib/postgresql/18
    printf "%s\n" "$output"
'
echo "PASS: PG17 data refuses unchanged with README migration guidance."
finish_case

start_case symlink
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "Symlink found" '
    ln -s /tmp/missing-target /var/lib/postgresql/18
    output=$(/usr/local/bin/prepare-data 2>&1) && exit 1
    test -L /var/lib/postgresql/18
    test "$(readlink /var/lib/postgresql/18)" = /tmp/missing-target
    test ! -e /var/lib/postgresql/18/docker
    printf "%s\n" "$output"
'
echo "PASS: symlink refuses without being followed."
finish_case

start_case partial
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "README.md" '
    mkdir -p /var/lib/postgresql/18/docker
    postgres_uid=$(id -u postgres)
    postgres_gid=$(id -g postgres)
    chown "$postgres_uid:$postgres_gid" /var/lib/postgresql /var/lib/postgresql/18 /var/lib/postgresql/18/docker
    chmod 0700 /var/lib/postgresql /var/lib/postgresql/18 /var/lib/postgresql/18/docker
    printf "partial\n" > /var/lib/postgresql/18/docker/.partial
    chown "$postgres_uid:$postgres_gid" /var/lib/postgresql/18/docker/.partial
    before=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql /var/lib/postgresql/18 /var/lib/postgresql/18/docker /var/lib/postgresql/18/docker/.partial; sha256sum /var/lib/postgresql/18/docker/.partial)
    output=$(/usr/local/bin/prepare-data 2>&1) && exit 1
    after=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql /var/lib/postgresql/18 /var/lib/postgresql/18/docker /var/lib/postgresql/18/docker/.partial; sha256sum /var/lib/postgresql/18/docker/.partial)
    test "$before" = "$after"
    test ! -e /var/lib/postgresql/18/docker/PG_VERSION
    printf "%s\n" "$output"
'
echo "PASS: partial PG18 data refuses unchanged."
finish_case

start_case unknown
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "Unexpected entry" '
    printf "preserve-me\n" > /var/lib/postgresql/.unrecognized
    before_root=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql)
    before_file=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql/.unrecognized)
    before_hash=$(sha256sum /var/lib/postgresql/.unrecognized)
    output=$(/usr/local/bin/prepare-data 2>&1) && exit 1
    test "$before_root" = "$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql)"
    test "$before_file" = "$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql/.unrecognized)"
    test "$before_hash" = "$(sha256sum /var/lib/postgresql/.unrecognized)"
    test "$(cat /var/lib/postgresql/.unrecognized)" = preserve-me
    test ! -e /var/lib/postgresql/18
    printf "%s\n" "$output"
'
echo "PASS: unknown root dotfile refuses unchanged."
finish_case

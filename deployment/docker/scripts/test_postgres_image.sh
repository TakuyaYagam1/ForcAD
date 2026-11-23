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
    test "$PGDATA" = /var/lib/postgresql/19/docker
    test "$(stat -c %u "${PGDATA%/*}")" = "$(id -u)"
    test "$(stat -c %a "${PGDATA%/*}")" = 700
    test "$(stat -c %u "$PGDATA")" = "$(id -u)"
    test "$(stat -c %a "$PGDATA")" = 700
    : > "$PGDATA/.access-check"
    rm "$PGDATA/.access-check"
'
echo "PASS: fresh root-owned mode-0700 tmpfs is accessible by postgres."

docker exec --detach --user "$pg_uid:$pg_gid" \
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
    'test "$(stat -c %u "$PGDATA/postmaster.pid")" = "$(id -u postgres)"'
docker exec --user "$pg_uid:$pg_gid" "$container_id" \
    pg_ctl -m fast -w stop >/dev/null
echo "PASS: PostgreSQL starts as nonroot on the adopted data root."

cluster_state() {
    docker exec "$container_id" /bin/sh -ec '
        stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql "${PGDATA%/*}" "$PGDATA" "$PGDATA/PG_VERSION" "$PGDATA/global/pg_control"
        sha256sum "$PGDATA/PG_VERSION" "$PGDATA/global/pg_control"
    '
}
before_state="$(cluster_state)"
docker exec "$container_id" /usr/local/bin/prepare-data
[[ "$before_state" == "$(cluster_state)" ]] || fail "Valid PostgreSQL data changed during prepare-data."
echo "PASS: existing valid cluster remains unchanged."
finish_case

start_case legacy
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "Legacy PostgreSQL data" '
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
    test ! -e "${PGDATA%/*}"
    printf "%s\n" "$output"
'
echo "PASS: legacy flat data is preserved."
finish_case

start_case previous-major
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "Legacy PostgreSQL data" '
    old=/var/lib/postgresql/18/docker
    mkdir -p "$old"
    printf "18\n" > "$old/PG_VERSION"
    before=$(stat -c "%u:%g:%a:%Y:%Z" "${old%/*}" "$old" "$old/PG_VERSION")
    output=$(/usr/local/bin/prepare-data 2>&1) && exit 1
    test "$before" = "$(stat -c "%u:%g:%a:%Y:%Z" "${old%/*}" "$old" "$old/PG_VERSION")"
    test "$(cat "$old/PG_VERSION")" = 18
    test ! -e "${PGDATA%/*}"
    printf "%s\n" "$output"
'
echo "PASS: an 18.x volume is refused before creating a 19.x directory."
finish_case

start_case symlink
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "Symlink found" '
    ln -s /tmp/missing-target "${PGDATA%/*}"
    output=$(/usr/local/bin/prepare-data 2>&1) && exit 1
    test -L "${PGDATA%/*}"
    test "$(readlink "${PGDATA%/*}")" = /tmp/missing-target
    test ! -e "$PGDATA"
    printf "%s\n" "$output"
'
echo "PASS: symlink refuses without being followed."
finish_case

start_case partial
# Variables expand in the container shell, not on the host.
# shellcheck disable=SC2016
expect_refusal "Unrecognized or incomplete PostgreSQL data" '
    mkdir -p "$PGDATA"
    postgres_uid=$(id -u postgres)
    postgres_gid=$(id -g postgres)
    chown "$postgres_uid:$postgres_gid" /var/lib/postgresql "${PGDATA%/*}" "$PGDATA"
    chmod 0700 /var/lib/postgresql "${PGDATA%/*}" "$PGDATA"
    printf "partial\n" > "$PGDATA/.partial"
    chown "$postgres_uid:$postgres_gid" "$PGDATA/.partial"
    before=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql "${PGDATA%/*}" "$PGDATA" "$PGDATA/.partial"; sha256sum "$PGDATA/.partial")
    output=$(/usr/local/bin/prepare-data 2>&1) && exit 1
    after=$(stat -c "%u:%g:%a:%Y:%Z" /var/lib/postgresql "${PGDATA%/*}" "$PGDATA" "$PGDATA/.partial"; sha256sum "$PGDATA/.partial")
    test "$before" = "$after"
    test ! -e "$PGDATA/PG_VERSION"
    printf "%s\n" "$output"
'
echo "PASS: partial cluster data refuses unchanged."
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
    test ! -e "${PGDATA%/*}"
    printf "%s\n" "$output"
'
echo "PASS: unknown root dotfile refuses unchanged."
finish_case

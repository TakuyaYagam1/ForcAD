#!/usr/bin/env bash
set -euo pipefail

if (( $# > 1 )); then
    echo "Usage: $0 [pgbouncer-image]" >&2
    exit 2
fi
image="${1:-forcad-pgbouncer:latest}"
scripts="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
docker image inspect "$image" >/dev/null

# Exercise the shipped AWK implementation without starting PgBouncer or a DB.
docker run --rm --pull never --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges:true --user 10001:10001 \
    --tmpfs /run/pgbouncer:rw,nosuid,nodev,uid=10001,gid=10001,mode=0700,size=1m \
    --mount "type=bind,src=$scripts/pgbouncer/config.awk,dst=/tmp/config.awk,readonly" \
    --env POSTGRES_PASSWORD=pgbouncer-config-test \
    --entrypoint /bin/sh -i "$image" -s <<'SH'
set -eu
umask 077
config=/run/pgbouncer/pgbouncer.ini
unset PGBOUNCER_API_CONNECTIONS PGBOUNCER_API_MIN_CONNECTIONS
awk -f /tmp/config.awk
grep '^forcad_api = .* pool_size=32 max_db_connections=32 min_pool_size=0 ' "$config" >/dev/null
awk '$1 ~ /^forcad_/ && $1 != "forcad_api"' "$config" > /run/pgbouncer/other-roles

for setting in 0:32 1:32 32:32 0:7 7:7 65535:65535; do
    minimum=${setting%:*}
    maximum=${setting#*:}
    PGBOUNCER_API_MIN_CONNECTIONS="$minimum" PGBOUNCER_API_CONNECTIONS="$maximum" awk -f /tmp/config.awk
    grep "^forcad_api = .* pool_size=$maximum max_db_connections=$maximum min_pool_size=$minimum " "$config" >/dev/null
    awk '$1 ~ /^forcad_/ && $1 != "forcad_api"' "$config" > /run/pgbouncer/current-other-roles
    cmp /run/pgbouncer/other-roles /run/pgbouncer/current-other-roles
done

for minimum in -1 33 65536 999999999999999999999 invalid 1.5 1e1 '+1' ' 1' '1 '; do
    if PGBOUNCER_API_MIN_CONNECTIONS="$minimum" PGBOUNCER_API_CONNECTIONS=32 awk -f /tmp/config.awk > /run/pgbouncer/error.log; then
        echo 'FAIL: accepted invalid API pool minimum' >&2
        exit 1
    fi
    grep 'PGBOUNCER_API_MIN_CONNECTIONS' /run/pgbouncer/error.log >/dev/null
done
if PGBOUNCER_API_MIN_CONNECTIONS="$(printf '1\n2')" awk -f /tmp/config.awk > /run/pgbouncer/error.log; then
    echo 'FAIL: accepted multiline API pool minimum' >&2
    exit 1
fi
grep 'PGBOUNCER_API_MIN_CONNECTIONS' /run/pgbouncer/error.log >/dev/null
echo 'PASS: API pool minimum defaults, inclusive bounds, invalid inputs and unchanged other roles.'
SH

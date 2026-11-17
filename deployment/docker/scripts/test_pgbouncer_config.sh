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
FORCAD_WORKER_REPLICAS=20 awk -f /tmp/config.awk
grep '^forcad_worker = .* pool_size=32 max_db_connections=32 max_db_client_connections=328 ' "$config" >/dev/null
FORCAD_API_REPLICAS=0 FORCAD_RECEIVER_REPLICAS=0 FORCAD_TICKER_REPLICAS=0 FORCAD_WORKER_REPLICAS=20 awk -f /tmp/config.awk
for replicas in -1 1025 invalid; do
    if FORCAD_WORKER_REPLICAS="$replicas" awk -f /tmp/config.awk > /run/pgbouncer/error.log; then
        echo 'FAIL: accepted invalid replica count' >&2; exit 1
    fi
done
if FORCAD_WORKER_REPLICAS=20 PGBOUNCER_WORKER_CLIENTS=256 awk -f /tmp/config.awk > /run/pgbouncer/error.log; then
    echo 'FAIL: accepted worker clients below replica demand' >&2; exit 1
fi
if FORCAD_WORKER_REPLICAS=64 awk -f /tmp/config.awk > /run/pgbouncer/error.log; then
    echo 'FAIL: accepted aggregate demand beyond global client cap' >&2; exit 1
fi
if PGBOUNCER_SERVER_TLS_SSLMODE=verify-full awk -f /tmp/config.awk > /run/pgbouncer/error.log; then
    echo 'FAIL: accepted verified upstream TLS without a CA' >&2; exit 1
fi
if PGBOUNCER_CLIENT_TLS_SSLMODE=require awk -f /tmp/config.awk > /run/pgbouncer/error.log; then
    echo 'FAIL: accepted required client TLS without a certificate/key' >&2; exit 1
fi
printf 'config-test-placeholder\n' > /run/pgbouncer/ca.crt
PGBOUNCER_CLIENT_TLS_SSLMODE=require PGBOUNCER_CLIENT_TLS_CERT_FILE=/run/pgbouncer/ca.crt \
PGBOUNCER_CLIENT_TLS_KEY_FILE=/run/pgbouncer/ca.crt PGBOUNCER_SERVER_TLS_SSLMODE=verify-full \
PGBOUNCER_SERVER_TLS_CA_FILE=/run/pgbouncer/ca.crt awk -f /tmp/config.awk
grep '^server_tls_sslmode = verify-full$' "$config" >/dev/null
grep '^client_tls_sslmode = require$' "$config" >/dev/null

echo 'PASS: pool minimums, replica/client budgets, and fail-closed TLS configuration.'
SH

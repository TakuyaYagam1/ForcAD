#!/bin/sh
set -eu

public_scheme=${PUBLIC_SCHEME:-http}
case "$public_scheme" in
    http|https) ;;
    *)
        echo "PUBLIC_SCHEME must be http or https" >&2
        exit 1
        ;;
esac

umask 077
export FORCAD_FLAGS_IP_RPS="${FORCAD_FLAGS_IP_RPS:-400}"
export FORCAD_FLAGS_TOTAL_RPS="${FORCAD_FLAGS_TOTAL_RPS:-2000}"
export FORCAD_WS_PER_IP="${FORCAD_WS_PER_IP:-128}"
for limit in "$FORCAD_FLAGS_IP_RPS" "$FORCAD_FLAGS_TOTAL_RPS" "$FORCAD_WS_PER_IP"; do
    case "$limit" in
        0*|*[!0-9]*|??????*) echo "Ingress limits must be integers from 1 to 65535" >&2; exit 1 ;;
    esac
    if [ "$limit" -gt 65535 ]; then
        echo "Ingress limits must be integers from 1 to 65535" >&2
        exit 1
    fi
done
# shellcheck disable=SC2016 # envsubst takes literal variable names.
envsubst '${FORCAD_FLAGS_IP_RPS} ${FORCAD_FLAGS_TOTAL_RPS} ${FORCAD_WS_PER_IP}' \
    < /etc/nginx/http.conf > /tmp/forcad-http.conf

printf 'proxy_set_header X-Forwarded-Proto %s;\n' "$public_scheme" \
    > /tmp/forcad-proxy-proto
chmod 0644 /tmp/forcad-proxy-proto

: > /tmp/forcad-trusted-proxies.conf
for proxy_cidr in ${TRUSTED_PROXY_CIDRS:-}; do
    case "$proxy_cidr" in
        *[!0-9a-fA-F.:/]*|'')
            echo "TRUSTED_PROXY_CIDRS contains an invalid address: $proxy_cidr" >&2
            exit 1
            ;;
    esac
    printf 'set_real_ip_from %s;\n' "$proxy_cidr" \
        >> /tmp/forcad-trusted-proxies.conf
done
chmod 0644 /tmp/forcad-trusted-proxies.conf

write_admin_addresses() {
    setting=$1
    shift
    for address in "$@"; do
        case "$address" in
            ''|*[!0-9a-fA-F.:/]*)
                printf '%s expects IPv4 or IPv6 addresses or CIDRs\n' "$setting" >&2
                exit 1
                ;;
        esac
        printf '%s 1;\n' "$address"
    done
}

{
    if [ -n "${FORCAD_ADMIN_CIDR:-}" ]; then
        printf 'default 0;\n'
        write_admin_addresses FORCAD_ADMIN_CIDR "$FORCAD_ADMIN_CIDR"
    else
        printf 'default 1;\n'
    fi
} > /tmp/forcad-admin-network.conf

# Split the configured list on whitespace without expanding filenames.
set -f
# shellcheck disable=SC2086
write_admin_addresses FORCAD_ADMIN_DENY_CIDRS ${FORCAD_ADMIN_DENY_CIDRS:-} \
    > /tmp/forcad-admin-deny.conf
chmod 0644 /tmp/forcad-admin-network.conf /tmp/forcad-admin-deny.conf

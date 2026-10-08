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

#!/bin/sh
set -eu
case "$1" in
    router)
        forcad network-rules --config /app/network.json | nft -f -
        exec forcad network-metrics --config /app/network.json --listen 0.0.0.0:9109
        ;;
    team)
        ip route replace "$DEMO_PEER/32" via 172.30.240.254
        exec setpriv --reuid=10001 --regid=10001 --clear-groups \
            --bounding-set=-all --inh-caps=-all --ambient-caps=-all python service.py
        ;;
    traffic) exec python traffic.py ;;
    *) echo 'Expected router, team or traffic' >&2; exit 2 ;;
esac

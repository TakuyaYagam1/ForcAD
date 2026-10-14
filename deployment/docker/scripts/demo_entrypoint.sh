#!/bin/sh
set -eu
case "$1" in
    router|vpn|player|team|forcad)
        exec python network.py "$1"
        ;;
    traffic) exec python traffic.py ;;
    load) shift; exec python load.py "$@" ;;
    *) echo 'Unknown demo command' >&2; exit 2 ;;
esac

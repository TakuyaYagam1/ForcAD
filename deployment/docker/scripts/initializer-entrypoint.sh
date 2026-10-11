#!/bin/sh

set -eu

platform_python=/opt/forcad/bin/python
db_check=/usr/local/bin/db_check.py
initialized_check=/usr/local/bin/check_initialized.py

echo '[*] Waiting for PostgreSQL and RabbitMQ'
until "$platform_python" "$db_check"; do
    sleep 5
done

set +e
"$platform_python" "$initialized_check"
check_status=$?
set -e

case "$check_status" in
    0)
        echo '[+] Already initialized'
        exit 0
        ;;
    1)
        ;;
    *)
        echo '[!] Could not verify the current initialization state.' >&2
        exit "$check_status"
        ;;
esac

echo '[*] Initializing game'
exec "$platform_python" -m forcad.entrypoints initializer "$@"

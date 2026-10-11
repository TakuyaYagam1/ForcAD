#!/bin/sh

set -eu

platform_python=/opt/forcad/bin/python
db_check=/usr/local/bin/db_check.py
initialized_check=/usr/local/bin/check_initialized.py

if [ "$#" -lt 1 ]; then
    echo 'A ForcAD process role is required.' >&2
    exit 2
fi

role=$1
shift

case "$role" in
    api|admin|events|http_receiver|ticker|worker|flower)
        ;;
    *)
        echo "Unsupported ForcAD process role: $role" >&2
        exit 2
        ;;
esac

echo '[*] Waiting for PostgreSQL and RabbitMQ'
until "$platform_python" "$db_check"; do
    sleep 5
done

echo '[*] Waiting for the initializer'
while :; do
    set +e
    "$platform_python" "$initialized_check"
    check_status=$?
    set -e

    if [ "$check_status" -eq 0 ]; then
        break
    fi
    sleep 5
done

if [ "$role" = flower ]; then
    echo '[*] Waiting for a registered Celery worker'
    until "$platform_python" -m celery -A forcad.services.tasks.app:celery_app inspect registered >/dev/null 2>&1; do
        sleep 5
    done
fi

exec "$platform_python" -m forcad.entrypoints "$role" "$@"

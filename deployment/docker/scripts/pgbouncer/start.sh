#!/bin/sh
set -eu
umask 077

mkdir -p /run/pgbouncer
awk -f /usr/local/share/pgbouncer/config.awk
exec pgbouncer /run/pgbouncer/pgbouncer.ini

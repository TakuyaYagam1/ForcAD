#!/bin/sh
set -eu

data_dir=/var/lib/postgresql/data
postgres_uid=$(id -u postgres)
postgres_gid=$(id -g postgres)

test -d "$data_dir"
test ! -L "$data_dir"

# Only a fresh directory can be adopted. Never recursively change an existing DB.
if [ -z "$(find "$data_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    chown "$postgres_uid:$postgres_gid" "$data_dir"
elif [ "$(stat -c %u "$data_dir")" != "$postgres_uid" ]; then
    echo 'PostgreSQL data has an unexpected owner; refusing to change it.' >&2
    exit 1
fi
chmod 0700 "$data_dir"

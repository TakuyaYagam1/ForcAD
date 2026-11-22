#!/bin/sh
set -eu

data_root=/var/lib/postgresql
major=19
major_dir=$data_root/$major
data_dir=$major_dir/docker
postgres_uid=$(id -u postgres)
postgres_gid=$(id -g postgres)

test -d "$data_root"
test ! -L "$data_root"

refuse_symlink() {
    path=$1
    if [ -L "$path" ]; then
        echo "Symlink found at $path; refusing to prepare PostgreSQL data." >&2
        exit 1
    fi
}

refuse_legacy_data() {
    marker=$1
    if [ -e "$marker" ] || [ -L "$marker" ]; then
        echo "Legacy PostgreSQL data found at $marker; refusing to initialize PostgreSQL $major at $data_dir. The existing volume must be preserved." >&2
        exit 1
    fi
}

require_postgres_access() {
    path=$1
    required_bits=$2

    if [ "$(stat -c %u "$path")" != "$postgres_uid" ]; then
        echo "PostgreSQL path $path has an unexpected owner; refusing to change it." >&2
        exit 1
    fi

    mode=$((0$(stat -c %a "$path")))
    if [ $((mode & required_bits)) -ne $((required_bits)) ]; then
        echo "PostgreSQL path $path lacks required owner permissions; refusing to change it." >&2
        exit 1
    fi
}

refuse_legacy_data "$data_root/PG_VERSION"
refuse_legacy_data "$data_root/data/PG_VERSION"

for child in "$data_root"/* "$data_root"/.[!.]* "$data_root"/..?*; do
    [ -e "$child" ] || [ -L "$child" ] || continue
    refuse_symlink "$child"
    if [ -d "$child" ]; then
        refuse_symlink "$child/docker"
        [ "$child" = "$major_dir" ] || refuse_legacy_data "$child/docker/PG_VERSION"
    fi
    if [ "$child" != "$major_dir" ]; then
        echo "Unexpected entry at $child; refusing to initialize PostgreSQL $major on this volume." >&2
        exit 1
    fi
done

refuse_symlink "$major_dir"
refuse_symlink "$data_dir"

# A fresh bind mount is commonly root-owned and mode 0700. Adopt it only while
# it is empty, before creating the major-version directory below it.
if [ -z "$(find "$data_root" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    chown "$postgres_uid:$postgres_gid" "$data_root"
    chmod 0700 "$data_root"
else
    require_postgres_access "$data_root" 0100
fi

if [ ! -d "$major_dir" ]; then
    mkdir "$major_dir"
    chown "$postgres_uid:$postgres_gid" "$major_dir"
    chmod 0700 "$major_dir"
fi
test -d "$major_dir"
refuse_symlink "$major_dir"
require_postgres_access "$major_dir" 0100

if [ ! -d "$data_dir" ]; then
    mkdir "$data_dir"
fi
test -d "$data_dir"
refuse_symlink "$data_dir"

if [ -z "$(find "$data_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    chown "$postgres_uid:$postgres_gid" "$data_dir"
    chmod 0700 "$data_dir"
else
    if [ -L "$data_dir/PG_VERSION" ] || [ ! -f "$data_dir/PG_VERSION" ] \
        || [ "$(cat "$data_dir/PG_VERSION")" != "$major" ]; then
        echo "Unrecognized or incomplete PostgreSQL data at $data_dir; refusing to change it." >&2
        exit 1
    fi
    require_postgres_access "$data_dir" 0700
fi

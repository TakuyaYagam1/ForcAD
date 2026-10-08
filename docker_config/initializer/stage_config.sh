#!/bin/sh
set -eu

source_config=/source/config.yml
target_dir=/run/forcad-config
temporary_config="$target_dir/config.yml.$$"

rm -f "$target_dir"/config.yml.*
trap 'rm -f "$temporary_config"' EXIT
test -r "$source_config"
install -o 10001 -g 10001 -m 0400 "$source_config" "$temporary_config"
mv -f "$temporary_config" "$target_dir/config.yml"

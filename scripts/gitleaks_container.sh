#!/usr/bin/env bash
set -euo pipefail

: "${FORCAD_GITLEAKS_ROOT:?FORCAD_GITLEAKS_ROOT is required}"
: "${FORCAD_GITLEAKS_REPORTS:?FORCAD_GITLEAKS_REPORTS is required}"
: "${FORCAD_GITLEAKS_CACHE:?FORCAD_GITLEAKS_CACHE is required}"

scan_root="$(cd "$FORCAD_GITLEAKS_ROOT" && pwd -P)"
reports_dir="$(cd "$FORCAD_GITLEAKS_REPORTS" && pwd -P)"
cache_dir="$(cd "$FORCAD_GITLEAKS_CACHE" && pwd -P)"
working_dir="$(pwd -P)"
container_user="$(id -u):$(id -g)"
docker_security_options="$(docker info --format '{{json .SecurityOptions}}')"
case "$docker_security_options" in
  *name=rootless*) container_user=0:0 ;;
esac

case "$working_dir" in
  "$scan_root"|"$scan_root"/*|"$cache_dir"|"$cache_dir"/*) ;;
  *)
    echo "Gitleaks working directory is outside its read-only scan inputs." >&2
    exit 2
    ;;
esac

docker run --rm --network none --platform linux/amd64 \
  --user "$container_user" \
  --mount "type=bind,src=$scan_root,dst=$scan_root,readonly" \
  --mount "type=bind,src=$cache_dir,dst=$cache_dir,readonly" \
  --mount "type=bind,src=$reports_dir,dst=$reports_dir" \
  --workdir "$working_dir" \
  --env HOME=/tmp \
  ghcr.io/gitleaks/gitleaks:v8.30.1@sha256:c00b6bd0aeb3071cbcb79009cb16a60dd9e0a7c60e2be9ab65d25e6bc8abbb7f \
  "$@"

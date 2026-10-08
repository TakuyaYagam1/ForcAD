#!/usr/bin/env bash
set -euo pipefail

: "${FORCAD_GITLEAKS_ROOT:?FORCAD_GITLEAKS_ROOT is required}"
: "${FORCAD_GITLEAKS_REPORTS:?FORCAD_GITLEAKS_REPORTS is required}"
: "${FORCAD_GITLEAKS_CACHE:?FORCAD_GITLEAKS_CACHE is required}"

scan_root="$(cd "$FORCAD_GITLEAKS_ROOT" && pwd -P)"
reports_dir="$(cd "$FORCAD_GITLEAKS_REPORTS" && pwd -P)"
cache_dir="$(cd "$FORCAD_GITLEAKS_CACHE" && pwd -P)"
working_dir="$(pwd -P)"

case "$working_dir" in
  "$scan_root"|"$scan_root"/*|"$cache_dir"|"$cache_dir"/*) ;;
  *)
    echo "Gitleaks working directory is outside its read-only scan inputs." >&2
    exit 2
    ;;
esac

docker run --rm --network none --platform linux/amd64 \
  --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$scan_root,dst=$scan_root,readonly" \
  --mount "type=bind,src=$cache_dir,dst=$cache_dir,readonly" \
  --mount "type=bind,src=$reports_dir,dst=$reports_dir" \
  --workdir "$working_dir" \
  --env HOME=/tmp \
  ghcr.io/gitleaks/gitleaks:v8.30.0@sha256:105ac66a57b2bb8afb61a3b8a5dcc4817773d03724a7e8a515214cfe58225556 \
  "$@"

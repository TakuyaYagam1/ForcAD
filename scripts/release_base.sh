#!/usr/bin/env bash

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
source "${SCRIPTS_DIR}/vars.sh"

pushd "${BASE_DIR}" >/dev/null

IMAGE="${FORCAD_BASE_IMAGE:-forcad_base:local}"
PUSH=false
for arg in "$@"; do
  if [[ "${arg}" == "--push" ]]; then
    PUSH=true
    break
  fi
done

if [[ "${PUSH}" == true && ! "${IMAGE}" =~ ^ghcr\.io/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)+:[A-Za-z0-9][A-Za-z0-9._+-]*$ ]]; then
  echo "Push requires FORCAD_BASE_IMAGE=ghcr.io/<owner>/forcad_base:<version>." >&2
  exit 2
fi

echo "[*] Building ${IMAGE}, base dir ${BASE_DIR}"
if [[ "${PUSH}" == true ]]; then
  docker buildx build -t "${IMAGE}" -f docker_config/base_images/backend.Dockerfile "$@" "${BASE_DIR}"
else
  docker buildx build --load -t "${IMAGE}" -f docker_config/base_images/backend.Dockerfile "$@" "${BASE_DIR}"
fi

popd >/dev/null

echo "[+] Done!"

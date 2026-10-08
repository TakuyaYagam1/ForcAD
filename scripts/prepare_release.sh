#!/bin/bash -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
source "${SCRIPTS_DIR}/vars.sh"

RELEASE_DIR="ForcAD_${VERSION}"

pushd "${BASE_DIR}" >/dev/null

rm -rf "${RELEASE_DIR}" "${RELEASE_DIR}.zip"

while read -r file; do
  echo "$file"
  mkdir -p "${RELEASE_DIR}/$(dirname "${file}")"
  cp -a "${file}" "${RELEASE_DIR}/${file}"
done <"${SCRIPTS_DIR}/BOM.txt"

find "${RELEASE_DIR}" -type d \( \
  -name '__pycache__' -o \
  -name '.cache' -o \
  -name '.mypy_cache' -o \
  -name '.pytest_cache' -o \
  -name '.ruff_cache' -o \
  -name '.venv' -o \
  -name 'node_modules' -o \
  -name '.pnpm-store' \
\) -prune -exec rm -rf {} +

find "${RELEASE_DIR}" -type f \( \
  -name '*.pyc' -o \
  -name '*.pyo' -o \
  -name '*.env' -o \
  -name '.env' -o \
  -name '.env.*' -o \
  -name 'config.yml' -o \
  -name 'config_backup*.yml' -o \
  -name '*.tsbuildinfo' -o \
  -name '.DS_Store' \
\) -delete

sed -i \
  -e '/^This documentation is for the latest/d' \
  -e '/^> This documentation is for the latest/d' \
  "${RELEASE_DIR}/README.md"

zip -r "${RELEASE_DIR}.zip" "${RELEASE_DIR}"

popd >/dev/null

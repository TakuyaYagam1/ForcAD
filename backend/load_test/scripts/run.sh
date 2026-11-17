#!/usr/bin/env bash
set -euo pipefail
backend="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
export LOAD_CLIENT_BUCKETS="${LOAD_CLIENT_BUCKETS:-1}"
exec "$backend/integration_test/scripts/test_deployment.sh" --load

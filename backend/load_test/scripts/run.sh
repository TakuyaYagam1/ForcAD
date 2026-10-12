#!/usr/bin/env bash
set -euo pipefail
backend="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
exec "$backend/integration_test/scripts/test_deployment.sh" --load

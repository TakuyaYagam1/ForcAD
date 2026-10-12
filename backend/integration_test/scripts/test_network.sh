#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/forcad-network.XXXXXX")"
test_name="forcad-network-$(id -u)-$$"
image_created=0
cleanup() {
    result=$?
    trap - EXIT
    docker rm -f "$test_name" >/dev/null 2>&1 || true
    if (( image_created )); then docker image rm "$test_name" >/dev/null 2>&1 || true; fi
    rm -rf -- "$test_root"
    exit "$result"
}
if docker container inspect "$test_name" >/dev/null 2>&1 || docker image inspect "$test_name" >/dev/null 2>&1; then
    rmdir "$test_root"
    echo 'Refusing to reuse an existing network test resource.' >&2
    exit 2
fi
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

CGO_ENABLED=0 go -C "$repo_root/backend" build -o "$test_root/forcad" ./cmd/forcad
"$test_root/forcad" network-config --config "$repo_root/backend/integration_test/fixtures/config.yml" > "$test_root/network.json"
"$test_root/forcad" network-rules --config "$test_root/network.json" > "$test_root/network.nft"
image_created=1
docker build --tag "$test_name" "$repo_root/backend/integration_test/fixtures/network"
# NET_ADMIN is restricted to this disposable container's network namespace.
docker run --rm -i --name "$test_name" --network none --cap-add NET_ADMIN \
    --mount "type=bind,src=$test_root,dst=/work,readonly" "$test_name" sh -se <<'SH'
nft -c -f /work/network.nft
nft -f /work/network.nft
if nft -f /work/network.nft >/dev/null 2>&1; then
    echo 'Repeated rule installation unexpectedly succeeded.' >&2
    exit 1
fi
/work/forcad network-metrics --config /work/network.json >/tmp/exporter.log 2>&1 &
exporter=$!
trap 'kill "$exporter" 2>/dev/null || true; wait "$exporter" 2>/dev/null || true' EXIT
for attempt in $(seq 1 30); do
    if wget -qO /tmp/metrics http://127.0.0.1:9109/metrics 2>/dev/null; then break; fi
    sleep 0.1
done
grep -qx 'forcad_network_scrape_success 1' /tmp/metrics
test "$(grep -c '^forcad_team_network_.*_total{' /tmp/metrics)" -eq 8
nft delete table inet forcad_monitoring
wget -qO /tmp/metrics http://127.0.0.1:9109/metrics
grep -qx 'forcad_network_scrape_success 0' /tmp/metrics
if grep -q '^forcad_team_network_' /tmp/metrics; then
    echo 'Exporter fabricated counters after the table disappeared.' >&2
    exit 1
fi
echo 'Network checks passed: nft rules, repeat refusal, real counters and missing-table handling.'
kill "$exporter"
wait "$exporter"
trap - EXIT
SH

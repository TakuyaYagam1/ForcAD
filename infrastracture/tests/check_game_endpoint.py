"""Run only inside the isolated sensor test container."""

import subprocess
import time
import urllib.error
import urllib.request

process = subprocess.Popen(
    ["zeek", "-b", "-i", "lo", "game-metrics.zeek", "game-inventory.zeek"],
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
)
try:
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise AssertionError(process.communicate())
        try:
            with urllib.request.urlopen(
                "http://127.0.0.1:9911/metrics", timeout=1
            ) as response:
                metrics = response.read().decode()
            assert "forcad_game_http_requests_total{" in metrics
            assert 'source_team_ip="10.101.0.2"' in metrics
            heartbeat = next(
                line
                for line in metrics.splitlines()
                if line.startswith("forcad_game_sync_timestamp_seconds")
            )
            if abs(float(heartbeat.rsplit(" ", 1)[1]) - time.time()) < 10:
                break
        except OSError:
            pass
        time.sleep(0.1)
    else:
        raise AssertionError(
            "Prometheus endpoint or sensor heartbeat did not become ready"
        )
    print("Native Prometheus endpoint and main-loop heartbeat passed.")
finally:
    process.terminate()
    stdout, stderr = process.communicate(timeout=10)
    assert process.returncode == 0, (stdout, stderr)

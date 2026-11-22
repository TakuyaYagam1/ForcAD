"""Check a running sensor without adding a test interpreter to its image."""

import subprocess
import time


def check_game_endpoint(cid):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        response = subprocess.run(
            [
                "docker",
                "exec",
                cid,
                "curl",
                "-fsS",
                "--noproxy",
                "*",
                "--max-time",
                "1",
                "http://127.0.0.1:9911/metrics",
            ],
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )
        if not response.returncode:
            metrics = response.stdout
            assert "forcad_game_http_requests_total{" in metrics
            assert 'source_team_ip="10.183.101.2"' in metrics
            if any(
                abs(float(line.rsplit(" ", 1)[1]) - time.time()) < 10
                for line in metrics.splitlines()
                if line.startswith("forcad_game_sync_timestamp_seconds")
            ):
                break
        time.sleep(0.1)
    else:
        raise AssertionError("Prometheus endpoint or sensor heartbeat did not become ready")
    subprocess.run(
        ["docker", "stop", "-t", "10", cid],
        check=True,
        capture_output=True,
        timeout=15,
    )
    status = subprocess.check_output(["docker", "wait", cid], text=True, timeout=5)
    assert status.strip() == "0", status
    print("Native Prometheus endpoint and main-loop heartbeat passed.")

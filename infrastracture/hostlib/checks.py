"""Read-only endpoint checks for host bundles."""

import re
import time
import urllib.request


def http(url, metrics=None):
    # Do not send private management requests through a shell proxy setting.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=5) as response:
        if response.status != 200:
            raise ValueError(f"Endpoint unavailable: {url}")
        if metrics:
            body = response.read(8 * 1024 * 1024).decode()
            if metrics == "network-metrics":
                for name in (
                    "forcad_network_scrape_success",
                    "forcad_network_source_scrape_success",
                ):
                    if not re.search(rf"(?m)^{name}(?:\{{[^\n]*\}})? 1(?:\.0)?$", body):
                        raise ValueError(f"Exporter reports failed collection: {name}")
            else:
                samples = re.findall(
                    r"(?m)^forcad_game_sync_timestamp_seconds(?:\{[^\n]*\})? ([0-9.eE+-]+)$",
                    body,
                )
                if not samples or any(
                    not 0 <= time.time() - float(value) < 30 for value in samples
                ):
                    raise ValueError("Game sensor heartbeat is missing or stale")

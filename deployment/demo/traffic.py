"""Small, continuous demo traffic; never used by production or load tests."""
import json
import os
import time
import urllib.error
import urllib.request
from collections import deque

BOARD = "http://nginx:8080"
PEER = "http://" + os.environ["DEMO_PEER"] + ":8080"
TOKEN = os.environ["DEMO_TOKEN"]
INTERVAL_SECONDS = 1
RECENT_LIMIT = 2000


def request(url, flags=None):
    headers = {"Content-Type": "application/json"}
    if flags is not None:
        headers["X-Team-Token"] = TOKEN
    req = urllib.request.Request(url, headers=headers,
                                 data=json.dumps(flags).encode() if flags is not None else None,
                                 method="PUT" if flags is not None else "GET")
    with urllib.request.urlopen(req, timeout=3) as response:
        return json.load(response)


def main():
    recent = deque(maxlen=RECENT_LIMIT)
    iteration = 0
    while True:
        try:
            request(BOARD + "/api/client/scoreboard/")
            flags = [flag for flag in request(PEER + "/demo/flags") if flag not in recent]
            if flags:
                result = request(BOARD + "/flags/", flags[:100])
                accepted = sum("Flag accepted!" in item["msg"] for item in result)
                print(f"Submitted {len(result)} flags, accepted {accepted}", flush=True)
                recent.extend(flags[:100])
            # Keep the flags/s graph visible between rounds, including invalids.
            if iteration % 5 == 0:
                request(BOARD + "/flags/", ["demo-invalid"])
            iteration += 1
        except (OSError, ValueError, KeyError) as exc:
            print(f"Waiting for demo services: {type(exc).__name__}", flush=True)
        time.sleep(INTERVAL_SECONDS)


if __name__ == "__main__":
    main()

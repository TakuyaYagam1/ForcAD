"""Small, continuous demo traffic; never used by production or load tests."""

import json
import os
import socket
import time
import urllib.error
import urllib.request
from collections import deque

from websocket import connect, ping

BOARD = "http://10.0.2.10"
PEER = "http://" + os.environ["DEMO_PEER"] + ":8080"
TOKEN = os.environ["DEMO_TOKEN"]
INTERVAL_SECONDS = 1
RECENT_LIMIT = 2000


def request(url, flags=None):
    headers = {"Content-Type": "application/json"}
    if flags is not None:
        headers["X-Team-Token"] = TOKEN
    req = urllib.request.Request(
        url,
        headers=headers,
        data=json.dumps(flags).encode() if flags is not None else None,
        method="PUT" if flags is not None else "GET",
    )
    with urllib.request.urlopen(req, timeout=3) as response:
        return json.load(response)


def main():
    recent = deque(maxlen=RECENT_LIMIT)
    iteration = 0
    websocket = None
    while True:
        flags = []
        try:
            flags = [
                flag for flag in request(PEER + "/demo/flags") if flag not in recent
            ]
            if iteration % 5 == 0:
                if websocket is None:
                    websocket = connect(os.environ["DEMO_PEER"])
                ping(websocket)
                with socket.create_connection(
                    (os.environ["DEMO_PEER"], 9001), timeout=3
                ) as tcp:
                    tcp.sendall(b"demo")
                    tcp.recv(4)
                with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as udp:
                    udp.settimeout(3)
                    udp.sendto(b"demo", (os.environ["DEMO_PEER"], 9000))
                    udp.recvfrom(128)
        except (OSError, ValueError, KeyError, EOFError) as exc:
            if websocket is not None:
                websocket.close()
                websocket = None
            print(f"Waiting for demo services: {type(exc).__name__}", flush=True)
        # Game traffic continues even when the board's practice session is idle.
        try:
            request(BOARD + "/api/client/scoreboard/")
            if flags:
                result = request(BOARD + "/flags/", flags[:100])
                accepted = sum("Flag accepted!" in item["msg"] for item in result)
                print(f"Submitted {len(result)} flags, accepted {accepted}", flush=True)
                recent.extend(flags[:100])
            if iteration % 5 == 0:
                request(BOARD + "/flags/", ["demo-invalid"])
        except (OSError, ValueError, KeyError) as exc:
            if iteration % 30 == 0:
                print(f"Board session unavailable: {type(exc).__name__}", flush=True)
        iteration += 1
        time.sleep(INTERVAL_SECONDS)


if __name__ == "__main__":
    main()

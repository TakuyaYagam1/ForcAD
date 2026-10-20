"""Bounded HTTP load against the other demo team, with optional held WebSockets."""

import argparse
import json
import os
import time
import urllib.request

from websocket import connect, ping


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rps", type=int, default=20)
    parser.add_argument("--seconds", type=int, default=45)
    parser.add_argument("--ws", type=int, default=5)
    args = parser.parse_args()
    if (
        not 1 <= args.rps <= 100
        or not 1 <= args.seconds <= 300
        or not 0 <= args.ws <= 20
    ):
        parser.error("Limits: RPS 1..100, duration 1..300 seconds, WS 0..20")
    peer = os.environ["DEMO_PEER"]
    if peer not in {f"10.183.{number}.2" for number in range(101, 133)}:
        parser.error("Load is restricted to local demo vulnboxes")
    sockets = []
    successes = failures = 0
    started = time.monotonic()
    next_request = started
    next_ping = started
    try:
        for _ in range(args.ws):
            sockets.append(connect(peer))
        while time.monotonic() - started < args.seconds:
            now = time.monotonic()
            if now < next_request:
                time.sleep(min(next_request - now, 0.1))
                continue
            next_request = max(next_request + 1 / args.rps, now)
            try:
                with urllib.request.urlopen(
                    f"http://{peer}:8080/health", timeout=2
                ) as response:
                    json.load(response)
                successes += 1
            except OSError:
                failures += 1
            if now >= next_ping:
                for sock in sockets:
                    ping(sock)
                next_ping = now + 10
    finally:
        for sock in sockets:
            sock.close()
    print(
        json.dumps(
            {
                "target": peer,
                "requests": successes,
                "failed": failures,
                "seconds": round(time.monotonic() - started, 2),
                "websockets": len(sockets),
            }
        )
    )
    if failures:
        raise SystemExit(1)


if __name__ == "__main__":
    main()

"""Small connectivity probes executed inside a selected demo namespace."""

import argparse
import json
import socket
import urllib.request

from websocket import connect, ping


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=("http", "tcp", "udp", "ws"))
    parser.add_argument("host")
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--path", default="/health")
    args = parser.parse_args()
    try:
        if args.kind == "http":
            with urllib.request.urlopen(
                f"http://{args.host}:{args.port}{args.path}", timeout=2
            ) as response:
                body = response.read(1 << 20)
                result = {"status": response.status}
                if args.path == "/health":
                    result.update(json.loads(body))
        elif args.kind == "tcp":
            with socket.create_connection((args.host, args.port), timeout=2) as sock:
                if args.port == 9001:
                    sock.sendall(b"demo")
                    assert sock.recv(4) == b"demo"
                result = {"connected": True}
        elif args.kind == "udp":
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                sock.settimeout(2)
                sock.sendto(b"demo", (args.host, args.port))
                assert sock.recvfrom(128)[0] == b"demo"
                result = {"echo": True}
        else:
            with connect(args.host) as sock:
                ping(sock)
                result = {"upgrade": 101, "pong": True}
    except (OSError, ValueError, EOFError):
        print(json.dumps({"connected": False}))
        raise SystemExit(1) from None
    print(json.dumps(result))


if __name__ == "__main__":
    main()

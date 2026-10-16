"""Local load fixtures; team IPs are isolated namespaces, not remote targets."""

import importlib
import json
import re
import sqlite3
import sys
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


@contextmanager
def connect():
    db = sqlite3.connect("/data/service.db", timeout=2)
    db.row_factory = sqlite3.Row
    try:
        with db:
            yield db
    finally:
        db.close()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    def do_GET(self):
        self.handle_request()

    def do_POST(self):
        self.handle_request()

    def handle_request(self):
        status, response = 500, {"error": "fixture failure"}
        try:
            if self.path == "/health":
                status, response = 200, {"status": "ok"}
            else:
                team, _, path = self.path.strip("/").partition("/")
                if not re.fullmatch(r"192\.0\.2\.([1-9]|1[0-9]|20)", team):
                    raise ValueError("unknown test team")
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 <= length <= 4096:
                    raise ValueError("body too large")
                body = json.loads(self.rfile.read(length)) if length else {}
                with connect() as db:
                    status, response = self.server.service.handle(
                        db, team, self.command, path, body, self.headers
                    )
        except (ValueError, KeyError, TypeError):
            status, response = 400, {"error": "invalid request"}
        except sqlite3.Error as exc:
            print(type(exc).__name__, file=sys.stderr, flush=True)
        data = json.dumps(response).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def main():
    name = sys.argv[1]
    if name not in ("notes", "vault", "jobs"):
        raise SystemExit("unknown fixture")
    service = importlib.import_module(name)
    with connect() as db:
        db.execute("PRAGMA journal_mode=WAL")
        service.initialize(db)
    server = ThreadingHTTPServer(("0.0.0.0", 8080), Handler)
    server.service = service
    if name == "jobs":
        service.start_worker(connect)
    server.serve_forever()


if __name__ == "__main__":
    main()

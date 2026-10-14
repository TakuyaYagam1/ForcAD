"""Disposable notes service for the reference checker and local dashboard demo."""
import json
import re
import sqlite3
import time
from contextlib import closing
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DATABASE = "/data/note.db"
RETENTION_SECONDS = 3600
MAX_BODY_BYTES = 4096
FLAG = re.compile(r"[A-Z0-9]{31}=")


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_GET(self):
        self.respond()

    def do_PUT(self):
        self.respond()

    def do_DELETE(self):
        self.respond()

    def respond(self):
        try:
            with closing(sqlite3.connect(DATABASE, timeout=2)) as db, db:
                status, data = self.dispatch(db)
        except (ValueError, TypeError, KeyError, UnicodeError):
            status, data = 400, {"error": "invalid request"}
        except sqlite3.Error:
            status, data = 503, {"error": "storage unavailable"}
        payload = json.dumps(data).encode() if status != 204 else b""
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def dispatch(self, db):
        if self.command == "GET" and self.path == "/health":
            return 200, {"status": "ok"}
        # Intentionally public in this demo only, so farms can steal real flags.
        if self.command == "GET" and self.path == "/demo/flags":
            rows = db.execute("SELECT title,body FROM note WHERE created > ? ORDER BY created DESC LIMIT 100", (time.time() - 300,))
            return 200, [value for row in rows for value in row if FLAG.fullmatch(value)]
        match = re.fullmatch(r"/notes/([0-9a-f]{40})", self.path)
        if not match:
            return 404, {"error": "not found"}
        note_id = match[1]
        authorization = self.headers.get("Authorization", "")
        if not authorization.startswith("Bearer ") or not authorization[7:]:
            return 401, {"error": "token required"}
        token = authorization[7:]
        if self.command == "PUT":
            return self.create(db, note_id, token)
        row = db.execute("SELECT title,body,token FROM note WHERE id=?", (note_id,)).fetchone()
        if row is None:
            return 404, {"error": "not found"}
        if row[2] != token:
            return 403, {"error": "wrong token"}
        if self.command == "GET":
            return 200, {"id": note_id, "title": row[0], "body": row[1]}
        db.execute("DELETE FROM note WHERE id=?", (note_id,))
        return 204, None

    def create(self, db, note_id, token):
        length = int(self.headers.get("Content-Length", "0"))
        if not 0 < length <= MAX_BODY_BYTES:
            raise ValueError("invalid body length")
        data = json.loads(self.rfile.read(length))
        if not isinstance(data, dict) or any(not isinstance(data.get(key), str) for key in ("title", "body")):
            raise ValueError("invalid note")
        db.execute("DELETE FROM note WHERE created < ?", (time.time() - RETENTION_SECONDS,))
        try:
            db.execute("INSERT INTO note VALUES(?,?,?,?,?)", (note_id, data["title"], data["body"], token, time.time()))
        except sqlite3.IntegrityError:
            return 409, {"error": "already exists"}
        return 201, {"id": note_id}


if __name__ == "__main__":
    with closing(sqlite3.connect(DATABASE)) as db, db:
        db.execute("PRAGMA journal_mode=WAL")
        db.execute("CREATE TABLE IF NOT EXISTS note(id TEXT PRIMARY KEY,title TEXT,body TEXT,token TEXT,created REAL)")
    server = ThreadingHTTPServer(("0.0.0.0", 8080), Handler)
    server.serve_forever()

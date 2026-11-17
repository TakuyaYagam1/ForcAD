"""Exercise the final Kincare checker through its subprocess protocol."""

import json
import os
from pathlib import Path
import subprocess
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread
import unittest


CHECKER = Path(__file__).resolve().parents[2] / "checkers/kincare/checker.py"
CARD_ID = "12345678-1234-1234-1234-123456789abc"
PATIENT_ID = "87654321-1234-1234-1234-123456789abc"
FLAG = "K" + "A" * 30 + "="


class KincareHandler(BaseHTTPRequestHandler):
    flag: str = ""

    def log_message(self, format, *args):
        pass

    def respond(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if self.path == "/auth/register":
            self.respond(201, {"id": CARD_ID, "self_patient_id": PATIENT_ID})
        elif self.path == "/auth/login":
            self.respond(200, {"token": "local-test-token"})
        elif self.path == "/auth/logout":
            self.respond(200, {})
        else:
            self.respond(404, {})

    def do_PUT(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path == "/family/cards":
            type(self).flag = body["private_context"]
            self.respond(201, {"id": CARD_ID})
        else:
            self.respond(404, {})

    def do_GET(self):
        if self.path == f"/family/cards/{CARD_ID}":
            self.respond(
                200,
                {"id": CARD_ID, "revision": 1, "private_context": type(self).flag},
            )
        else:
            self.respond(404, {})


class FinalCheckerTest(unittest.TestCase):
    def setUp(self):
        KincareHandler.flag = ""
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), KincareHandler)
        self.thread = Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def invoke(self, *arguments):
        return subprocess.run(
            [sys.executable, str(CHECKER), *arguments],
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
            env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"},
        )

    def test_put_returns_public_hint_and_private_state_then_get_recovers_flag(self):
        target = f"127.0.0.1:{self.server.server_port}"
        put = self.invoke("put", target, "a" * 40, FLAG, "1")
        self.assertEqual(put.returncode, 101, put.stdout + put.stderr)
        self.assertEqual(json.loads(put.stdout), {"place": 1, "family_card_id": CARD_ID})
        state = json.loads(put.stderr)
        self.assertEqual(set(state), {"email", "password", "id", "place"})
        self.assertEqual(state["id"], CARD_ID)

        get = self.invoke("get", target, put.stderr.strip(), FLAG, "1")
        self.assertEqual((get.returncode, get.stdout, get.stderr), (101, "OK\n", ""))

        corrupt = self.invoke("get", target, put.stderr.strip(), "K" + "B" * 30 + "=", "1")
        self.assertEqual((corrupt.returncode, corrupt.stdout), (102, "CORRUPT\n"))

    def test_rejects_invalid_place_without_network_side_effects(self):
        result = self.invoke("put", f"127.0.0.1:{self.server.server_port}", "a" * 40, FLAG, "6")
        self.assertEqual(result.returncode, 110)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()

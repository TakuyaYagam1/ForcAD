#!/usr/bin/env python3
"""Serve a built demo frontend as a local-network-only static SPA."""

from __future__ import annotations

import argparse
import mimetypes
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import unquote, urlsplit


mimetypes.add_type("font/woff2", ".woff2")


class DemoHandler(SimpleHTTPRequestHandler):
    server_version = "ForcADLocalDemo/1.0"

    def __init__(self, *args, directory: str, **kwargs):
        self.build_root = Path(directory).resolve(strict=True)
        super().__init__(*args, directory=str(self.build_root), **kwargs)

    def do_GET(self) -> None:
        self._serve_static()

    def do_HEAD(self) -> None:
        self._serve_static()

    def _serve_static(self) -> None:
        request_path = unquote(urlsplit(self.path).path)
        normalized = request_path.rstrip("/") or "/"

        # This server intentionally has no API or WebSocket implementation.
        if normalized == "/api" or normalized.startswith("/api/"):
            self.send_error(404, "No API is served by the local demo")
            return

        candidate = (self.build_root / normalized.lstrip("/")).resolve()
        try:
            candidate.relative_to(self.build_root)
        except ValueError:
            self.send_error(404, "Not found")
            return

        if candidate.is_dir():
            candidate = candidate / "index.html"
        if not candidate.is_file():
            # Missing asset-like paths should remain 404; client-side routes
            # without a file extension receive the SPA entry point.
            leaf = Path(normalized).name
            if Path(leaf).suffix:
                self.send_error(404, "Not found")
                return
            candidate = self.build_root / "index.html"

        try:
            candidate.relative_to(self.build_root)
        except ValueError:
            self.send_error(404, "Not found")
            return

        candidate = candidate.resolve()
        if not candidate.is_relative_to(self.build_root):
            self.send_error(404, "Not found")
            return

        # Delegate MIME and HEAD behavior to the stdlib handler.
        self.path = "/" + candidate.relative_to(self.build_root).as_posix()
        super().do_GET() if self.command == "GET" else super().do_HEAD()

    def end_headers(self) -> None:
        if urlsplit(self.path).path.endswith("index.html"):
            self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def log_message(self, format: str, *args) -> None:
        # Include requests in the runner log without logging headers/cookies.
        super().log_message(format, *args)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", type=int, default=5173)
    args = parser.parse_args()

    root = Path(args.root).resolve(strict=True)
    if not root.is_dir() or not (root / "index.html").is_file():
        raise SystemExit(f"Build root must contain index.html: {root}")

    server = ThreadingHTTPServer((args.host, args.port), lambda *a, **kw: DemoHandler(
        *a, directory=str(root), **kw
    ))
    server.daemon_threads = True
    print(f"Serving static demo from {root} on http://{args.host}:{args.port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()

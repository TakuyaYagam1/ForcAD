"""Management endpoint boundary tests; all requests stay on loopback."""

import threading
import unittest
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import MagicMock, patch

from hostlib import checks


class EndpointTests(unittest.TestCase):
    def test_private_targets_are_allowed_without_network_lookup(self):
        for endpoint in (
            "http://10.20.30.40:9109/metrics",
            "http://[fd00::12]:9109/metrics",
            "https://metrics.example.invalid/metrics",
        ):
            checks._validate_endpoint(endpoint)

    def test_invalid_authorities_are_rejected_before_open(self):
        with patch.object(checks.urllib.request, "build_opener") as build:
            for endpoint in (
                "file:///metrics", "//metrics.example.invalid/metrics",
                "http://user:private-marker@metrics.example.invalid/metrics",
                "http://metrics.example.invalid/metrics#fragment",
                "http://metrics.example.invalid:0/metrics",
                "http://metrics.example.invalid:65536/metrics",
                "http://metrics.example.invalid:/metrics",
            ):
                with self.subTest(endpoint=endpoint):
                    with self.assertRaisesRegex(ValueError, "Expected an HTTP") as error:
                        checks.http(endpoint)
                    self.assertNotIn("private-marker", str(error.exception))
            build.assert_not_called()

    def test_healthcheck_is_direct_and_bounded(self):
        response = MagicMock()
        response.status = 200
        response.__enter__.return_value = response
        opener = MagicMock()
        opener.open.return_value = response
        with patch.object(checks.urllib.request, "build_opener", return_value=opener) as build:
            checks.http("http://10.20.30.40:9109/metrics")
        handlers = build.call_args.args
        proxy = next(handler for handler in handlers if isinstance(handler, urllib.request.ProxyHandler))
        self.assertEqual(proxy.proxies, {})
        self.assertTrue(any(isinstance(handler, checks._NoRedirect) for handler in handlers))
        opener.open.assert_called_once_with("http://10.20.30.40:9109/metrics", timeout=5)

    def test_redirect_does_not_fetch_another_endpoint(self):
        paths = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                paths.append(self.path)
                self.send_response(302 if self.path == "/metrics" else 200)
                if self.path == "/metrics":
                    self.send_header("Location", "/other")
                self.send_header("Content-Length", "0")
                self.end_headers()

            def log_message(self, *_args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True)
        worker.start()
        try:
            with self.assertRaises(urllib.error.HTTPError) as error:
                checks.http(f"http://127.0.0.1:{server.server_port}/metrics")
            self.assertEqual(error.exception.code, 302)
            error.exception.close()
            self.assertEqual(paths, ["/metrics"])
        finally:
            server.shutdown()
            server.server_close()
            worker.join(timeout=2)


if __name__ == "__main__":
    unittest.main()

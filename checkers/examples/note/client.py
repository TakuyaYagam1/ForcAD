"""HTTP boundary for the example notes API. No redirects or automatic retries."""

import http.client
import ipaddress
import json
import time
from urllib.parse import urlsplit

from protocol import Failure, State, Status

DEFAULT_PORT = 8080
REQUEST_TIMEOUT = 2.0
ACTION_BUDGET = 8.0
MAX_RESPONSE_BYTES = 64 * 1024
READ_CHUNK_BYTES = 4096


class Client:
    def __init__(self, host: str):
        self.host, self.port = self.address(host)
        self.deadline = time.monotonic() + ACTION_BUDGET

    @staticmethod
    def address(host: str) -> tuple[str, int]:
        try:
            return str(ipaddress.ip_address(host)), DEFAULT_PORT
        except ValueError:
            pass
        try:
            address = urlsplit(f"http://{host}")
            if (
                not address.hostname
                or address.username is not None
                or address.password is not None
                or address.path
                or address.query
                or address.fragment
                or address.port == 0
            ):
                raise ValueError("invalid host")
            return address.hostname, address.port or DEFAULT_PORT
        except ValueError as exc:
            raise Failure(
                Status.CHECK_FAILED,
                "Checker invocation failed",
                "Client.address - expected a host or host:port",
            ) from exc

    def timeout(self) -> float:
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("action deadline exceeded")
        return min(REQUEST_TIMEOUT, remaining)

    def request(
        self,
        method: str,
        state: State,
        expected: int,
        payload: dict | None = None,
        *,
        stored: bool = False,
    ) -> dict:
        connection = http.client.HTTPConnection(self.host, self.port)
        try:
            connection.timeout = self.timeout()
            connection.connect()
            # HTTPResponse may own the socket after Connection: close.
            transport = connection.sock
            headers = {
                "Accept": "application/json",
                "Authorization": f"Bearer {state.token}",
            }
            body = None
            if payload is not None:
                body = json.dumps(payload).encode()
                headers["Content-Type"] = "application/json"
            transport.settimeout(self.timeout())
            connection.request(
                method, f"/notes/{state.note_id}", body=body, headers=headers
            )
            transport.settimeout(self.timeout())
            with connection.getresponse() as response:
                self.validate_status(response.status, expected, stored)
                if expected == 204:
                    return {}
                return self.read_json(response, transport)
        except (OSError, http.client.RemoteDisconnected) as exc:
            raise Failure(
                Status.DOWN,
                "Service unavailable",
                f"Client.request - {method} /notes/:id failed ({type(exc).__name__})",
            ) from exc
        except http.client.HTTPException as exc:
            raise Failure(
                Status.MUMBLE,
                "Invalid service response",
                f"Client.request - invalid HTTP response ({type(exc).__name__})",
            ) from exc
        finally:
            connection.close()

    @staticmethod
    def validate_status(status: int, expected: int, stored: bool) -> None:
        if status == expected:
            return
        if stored and status in {401, 403, 404, 410}:
            raise Failure(
                Status.CORRUPT,
                "Stored flag is unavailable",
                f"Client.validate_status - record is inaccessible (HTTP {status})",
            )
        verdict = Status.DOWN if status == 429 or 500 <= status < 600 else Status.MUMBLE
        raise Failure(
            verdict,
            "Unexpected service response",
            f"Client.validate_status - expected HTTP {expected}, received {status}",
        )

    def read_json(self, response, transport) -> dict:
        if response.headers.get_content_type() != "application/json":
            raise Failure(
                Status.MUMBLE,
                "Invalid service response",
                "Client.read_json - expected application/json",
            )
        content = bytearray()
        while not response.isclosed() and len(content) <= MAX_RESPONSE_BYTES:
            transport.settimeout(self.timeout())
            chunk = response.read1(
                min(READ_CHUNK_BYTES, MAX_RESPONSE_BYTES + 1 - len(content))
            )
            if not chunk:
                break
            content.extend(chunk)
        if len(content) > MAX_RESPONSE_BYTES:
            raise Failure(
                Status.MUMBLE,
                "Invalid service response",
                "Client.read_json - response exceeds size limit",
            )
        try:
            data = json.loads(content.decode("utf-8"))
            if not isinstance(data, dict):
                raise TypeError("expected object")
        except (ValueError, TypeError, RecursionError) as exc:
            raise Failure(
                Status.MUMBLE,
                "Invalid service response",
                "Client.read_json - expected a UTF-8 JSON object",
            ) from exc
        return data

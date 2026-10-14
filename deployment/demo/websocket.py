"""Small bounded WebSocket echo fixture and client for the local demo."""

import base64
import hashlib
import secrets
import socket

GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"


def accept_key(key):
    return base64.b64encode(
        hashlib.sha1((key + GUID).encode(), usedforsecurity=False).digest()
    ).decode()


def receive(sock, length):
    result = bytearray()
    while len(result) < length:
        chunk = sock.recv(length - len(result))
        if not chunk:
            raise EOFError("WebSocket closed")
        result.extend(chunk)
    return bytes(result)


def read_frame(sock, masked):
    head = receive(sock, 2)
    length = head[1] & 127
    if (
        not head[0] & 128
        or head[0] & 112
        or bool(head[1] & 128) != masked
        or length > 125
    ):
        raise ValueError("Demo accepts only complete frames up to 125 bytes")
    key = receive(sock, 4) if masked else None
    payload = receive(sock, length)
    if key:
        payload = bytes(value ^ key[index % 4] for index, value in enumerate(payload))
    return head[0] & 15, payload


def send_frame(sock, opcode, payload=b"", masked=False):
    if len(payload) > 125:
        raise ValueError("Demo frame too large")
    key = secrets.token_bytes(4) if masked else b""
    body = (
        bytes(value ^ key[index % 4] for index, value in enumerate(payload))
        if masked
        else payload
    )
    sock.sendall(
        bytes([128 | opcode, len(payload) | (128 if masked else 0)]) + key + body
    )


def connect(host):
    sock = socket.create_connection((host, 8081), timeout=3)
    try:
        key = base64.b64encode(secrets.token_bytes(16)).decode()
        sock.sendall(
            (
                f"GET /ws HTTP/1.1\r\nHost: {host}:8081\r\nUpgrade: websocket\r\n"
                f"Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
            ).encode()
        )
        header = bytearray()
        while not header.endswith(b"\r\n\r\n") and len(header) < 4096:
            header.extend(receive(sock, 1))
        if (
            not header.startswith(b"HTTP/1.1 101 ")
            or accept_key(key).encode() not in header
        ):
            raise ValueError("WebSocket upgrade failed")
        return sock
    except BaseException:
        sock.close()
        raise


def ping(sock):
    send_frame(sock, 9, b"demo", masked=True)
    if read_frame(sock, masked=False) != (10, b"demo"):
        raise ValueError("Missing WebSocket pong")

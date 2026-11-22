"""Small synthetic Ethernet traces for passive game-metric acceptance tests."""

import gzip
import ipaddress
import struct


def checksum(data):
    data += b"\0" * (len(data) % 2)
    total = sum(struct.unpack(f"!{len(data) // 2}H", data))
    while total >> 16:
        total = (total & 65535) + (total >> 16)
    return (~total) & 65535


class Trace:
    def __init__(self):
        self.frames = []

    def packet(
        self, source, target, sport, dport, data=b"", seq=0, ack=0, flags=24, udp=False
    ):
        source = ipaddress.ip_address(source).packed
        target = ipaddress.ip_address(target).packed
        proto = 17 if udp else 6
        if udp:
            segment = struct.pack("!HHHH", sport, dport, 8 + len(data), 0) + data
            offset = 6
        else:
            segment = (
                struct.pack(
                    "!HHIIBBHHH", sport, dport, seq, ack, 80, flags, 65535, 0, 0
                )
                + data
            )
            offset = 16
        pseudo = source + target + struct.pack("!BBH", 0, proto, len(segment))
        segment = (
            segment[:offset]
            + struct.pack("!H", checksum(pseudo + segment))
            + segment[offset + 2 :]
        )
        header = struct.pack(
            "!BBHHHBBH4s4s",
            69,
            0,
            20 + len(segment),
            len(self.frames),
            0,
            64,
            proto,
            0,
            source,
            target,
        )
        header = header[:10] + struct.pack("!H", checksum(header)) + header[12:]
        self.frames.append(
            bytes.fromhex("0200000000010200000000020800") + header + segment
        )

    def write(self, path, age_seconds=0):
        with path.open("wb") as stream:
            stream.write(struct.pack("<IHHIIII", 0xA1B2C3D4, 2, 4, 0, 0, 65535, 1))
            for index, frame in enumerate(self.frames):
                age = age_seconds if index == len(self.frames) - 1 else 0
                stream.write(
                    struct.pack(
                        "<IIII",
                        1700000000 + index // 100 + age,
                        (index % 100) * 10000,
                        len(frame),
                        len(frame),
                    )
                )
                stream.write(frame)


class TCP:
    def __init__(
        self,
        trace,
        source="10.183.200.2",
        target="10.183.102.2",
        port=8080,
        handshake=True,
    ):
        self.trace, self.source, self.target, self.port = trace, source, target, port
        self.sport = 20000 + len(trace.frames)
        self.seq, self.ack = 100, 500
        self.send(flags=2)
        self.seq += 1
        if handshake:
            self.send(reply=True, flags=18)
            self.ack += 1
            self.send(flags=16)

    def send(self, data=b"", reply=False, flags=24, offset=0):
        source, target, sport, dport, seq, ack = (
            self.source,
            self.target,
            self.sport,
            self.port,
            self.seq + offset,
            self.ack,
        )
        if reply:
            source, target, sport, dport, seq, ack = (
                target,
                source,
                dport,
                sport,
                self.ack,
                self.seq,
            )
        self.trace.packet(source, target, sport, dport, data, seq, ack, flags)

    def data(self, data, reply=False):
        self.send(data, reply)
        if reply:
            self.ack += len(data)
        else:
            self.seq += len(data)
        self.send(reply=not reply, flags=16)


    def data_segmented(self, data, reply=False, segment_size=60000):
        for start in range(0, len(data), segment_size):
            part = data[start : start + segment_size]
            self.send(part, reply)
            if reply:
                self.ack += len(part)
            else:
                self.seq += len(part)
            self.send(reply=not reply, flags=16)

    def close(self, reset=False):
        if reset:
            self.send(reply=True, flags=20)
            return
        self.send(flags=17)
        self.seq += 1
        self.send(reply=True, flags=17)
        self.ack += 1
        self.send(flags=16)


def fixture():
    trace = Trace()
    request = b"GET /sample HTTP/1.1\r\nHost: game\r\n\r\n"
    response = b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"
    first = TCP(trace)
    payload = request * 2
    # Missing middle arrives later. Retransmitting a segment is not a request.
    first.send(payload[:8])
    first.send(payload[24:], offset=24)
    first.send(payload[8:24], offset=8)
    first.send(payload[:8])
    first.seq += len(payload)
    first.send(reply=True, flags=16)
    first.data(response * 2, reply=True)
    first.data(
        b"POST /body HTTP/1.1\r\nHost: game\r\nContent-Length: "
        + str(len(request)).encode()
        + b"\r\n\r\n"
        + request
        + request
    )
    first.data(response * 2, reply=True)
    first.data(
        b"POST /chunked HTTP/1.1\r\nHost: game\r\nTransfer-Encoding: chunked\r\n\r\n"
        + f"{len(request):x}".encode()
        + b"\r\n"
        + request
        + b"\r\n0\r\n\r\n"
        + request
    )
    first.data(response * 2, reply=True)
    first.close()

    upgrade = b"GET /ws HTTP/1.1\r\nHost: game\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
    switched = b"HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n"
    for reset in (False, True):
        ws = TCP(trace, source="10.183.200.3", port=8081)
        ws.data(upgrade)
        ws.data(switched, reply=True)
        if reset:
            ws.close(reset=True)
    binary = TCP(trace, port=8081)
    binary.data(b"\x00\x01binary protocol\xff")
    binary.close(reset=True)
    denied = TCP(trace)
    denied.data(upgrade)
    denied.data(b"HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n", reply=True)
    denied.close()
    pending = TCP(trace, handshake=False)
    pending.seq -= 1
    pending.send(flags=2)
    for reply in (False, False, True):
        args = ("10.183.200.2", "10.183.102.2", 30000, 31337)
        if reply:
            args = (args[1], args[0], args[3], args[2])
        trace.packet(*args, data=b"udp", udp=True)

    reverse = TCP(trace, source="10.183.200.9", target="10.183.101.2")
    reverse.data(request)
    reverse.data(response, reply=True)
    reverse.close()
    for options in (
        {"source": "10.183.200.250"},
        {"source": "10.183.201.2"},
        {"source": "10.183.101.2"},
        {"target": "10.183.2.10"},
        {"port": 22},
    ):
        ignored = TCP(trace, **options)
        ignored.data(request)
        ignored.data(response, reply=True)
        ignored.close()
    return trace


def monitoring_fixture():
    trace = Trace()
    below_threshold = TCP(trace)
    body = b"a" * (1048576 - 1)
    below_threshold.data(
        b"POST /below HTTP/1.1\r\nHost: game\r\nContent-Length: "
        + str(len(body)).encode()
        + b"\r\n\r\n"
    )
    below_threshold.data_segmented(body)
    below_threshold.data(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", reply=True)
    below_threshold.close(reset=True)

    exact_threshold = TCP(trace)
    body = b"b" * 1048576
    exact_threshold.data(
        b"POST /exact HTTP/1.1\r\nHost: game\r\nContent-Length: "
        + str(len(body)).encode()
        + b"\r\n\r\n"
    )
    exact_threshold.data_segmented(body)
    exact_threshold.data(
        b"HTTP/1.1 429 Too Many Requests\r\nContent-Length: 0\r\n\r\n",
        reply=True,
    )
    exact_threshold.close()

    encoded = gzip.compress(b"g" * 1048576, mtime=0)
    compressed = TCP(trace)
    compressed.data(
        b"POST /gzip HTTP/1.1\r\nHost: game\r\n"
        b"Content-Encoding: gzip\r\nContent-Length: "
        + str(len(encoded)).encode()
        + b"\r\n\r\n"
        + encoded
    )
    compressed.data(
        b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n",
        reply=True,
    )
    compressed.close()

    chunked = TCP(trace)
    body = b"c" * 1048577
    chunks = []
    offset = 0
    for size in (400000, 400000, len(body) - 800000):
        part = body[offset : offset + size]
        chunks.extend((f"{len(part):x}\r\n".encode(), part, b"\r\n"))
        offset += size
    chunks.append(b"0\r\n\r\n")
    chunked.data(
        b"POST /chunked-large HTTP/1.1\r\nHost: game\r\n"
        b"Transfer-Encoding: chunked\r\n\r\n"
    )
    chunked.data_segmented(b"".join(chunks))
    chunked.data(b"HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n", reply=True)
    chunked.close()

    forged_length = TCP(trace)
    body = b"f" * 1048576
    forged_length.data(
        b"POST /truncated HTTP/1.1\r\nHost: game\r\n"
        b"Content-Length: 1048676\r\n\r\n"
    )
    # Parsed bytes meet the threshold, but the announced message is incomplete.
    forged_length.data_segmented(body)
    forged_length.close(reset=True)
    gap = TCP(trace)
    missing = 100
    prefix = b"d" * 800000
    suffix = b"e" * 300000
    gap.data(
        b"POST /gap HTTP/1.1\r\nHost: game\r\n"
        b"Content-Length: 1100100\r\n\r\n"
    )
    gap.data_segmented(prefix)
    # The capture omits bytes the sender transmitted; the server ACK confirms
    # they crossed the stream before the following observed payload.
    gap.seq += missing
    gap.send(reply=True, flags=16)
    gap.data_segmented(suffix)
    gap.close(reset=True)

    refused = TCP(trace, handshake=False)
    refused.send(reply=True, flags=20)
    TCP(trace, handshake=False)

    # A later packet advances capture time so Zeek can report the pending SYN.
    trace.packet("10.183.201.2", "10.183.102.2", 30000, 8080, flags=16)
    return trace

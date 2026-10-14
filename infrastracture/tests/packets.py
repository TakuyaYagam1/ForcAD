"""Exercise rendered rules with packets inside an isolated container network."""

import ipaddress
import select
import socket
import struct
import subprocess
import sys
import time
from pathlib import Path


def command(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout


def checksum(data):
    if len(data) % 2:
        data += b"\0"
    total = sum(struct.unpack(f"!{len(data) // 2}H", data))
    while total >> 16:
        total = (total & 65535) + (total >> 16)
    return (~total) & 65535


class Network:
    def __init__(self):
        self.links = {}
        self.serial = 1000
        command("ip", "link", "set", "lo", "up")
        # AF_PACKET sees received frames before this hook. Discard them there
        # so unaddressed test peers cannot route captured packets a second time.
        command("nft", "add", "table", "inet", "test_peers")
        command(
            "nft",
            "add",
            "chain",
            "inet",
            "test_peers",
            "input",
            "{ type filter hook prerouting priority -310; }",
        )
        command(
            "nft",
            "add",
            "rule",
            "inet",
            "test_peers",
            "input",
            "iifname",
            '"peer-*"',
            "drop",
        )

    def link(self, name, interface, address, neighbors):
        peer = f"peer-{name}"
        command("ip", "link", "add", interface, "type", "veth", "peer", "name", peer)
        command("ip", "address", "add", address, "dev", interface)
        for device in (interface, peer):
            command("ip", "link", "set", device, "up")
        mac = bytes.fromhex(
            Path(f"/sys/class/net/{interface}/address")
            .read_text()
            .strip()
            .replace(":", "")
        )
        peer_mac = Path(f"/sys/class/net/{peer}/address").read_text().strip()
        for neighbor in neighbors:
            command(
                "ip",
                "neigh",
                "replace",
                neighbor,
                "lladdr",
                peer_mac,
                "nud",
                "permanent",
                "dev",
                interface,
            )
        sock = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800))
        sock.bind((peer, 0))
        sock.setblocking(False)
        self.links[name] = (sock, mac, bytes.fromhex(peer_mac.replace(":", "")))

    def probe(
        self,
        incoming,
        outgoing,
        source,
        destination,
        port=8080,
        source_port=None,
        flags=2,
        sequence=100,
        ack=0,
        udp=False,
        allowed=True,
        translated_source=None,
        translated_destination=None,
        icmp_type=None,
        icmp_id=None,
    ):
        self.serial += 1
        serial = self.serial
        source_port = source_port or 30000 + serial
        src = ipaddress.IPv4Address(source).packed
        dst = ipaddress.IPv4Address(destination).packed
        protocol = 17 if udp else 6
        if icmp_type is not None:
            protocol = 1
            transport = struct.pack(
                "!BBHHH", icmp_type, 0, 0, icmp_id or serial, sequence
            ) + bytes(12)
            transport = (
                transport[:2] + struct.pack("!H", checksum(transport)) + transport[4:]
            )
        elif udp:
            transport = struct.pack("!HHHH", source_port, port, 8, 0)
        else:
            transport = struct.pack(
                "!HHIIBBHHH", source_port, port, sequence, ack, 80, flags, 8192, 0, 0
            )
            pseudo = src + dst + struct.pack("!BBH", 0, protocol, len(transport))
            transport = (
                transport[:16]
                + struct.pack("!H", checksum(pseudo + transport))
                + transport[18:]
            )
        header = struct.pack(
            "!BBHHHBBH4s4s",
            69,
            0,
            20 + len(transport),
            serial,
            0,
            64,
            protocol,
            0,
            src,
            dst,
        )
        header = header[:10] + struct.pack("!H", checksum(header)) + header[12:]
        sock, target_mac, peer_mac = self.links[incoming]
        receive = self.links[outgoing][0]
        while select.select([receive], [], [], 0)[0]:
            receive.recv(65535)
        sock.send(target_mac + peer_mac + b"\x08\x00" + header + transport)
        deadline = time.monotonic() + 0.15
        result = None
        while time.monotonic() < deadline:
            ready, _, _ = select.select(
                [receive], [], [], max(0, deadline - time.monotonic())
            )
            if not ready:
                break
            data, address = receive.recvfrom(65535)
            if address[2] == socket.PACKET_OUTGOING or len(data) < 42:
                continue
            if struct.unpack("!H", data[18:20])[0] != serial:
                continue
            result = {
                "source": socket.inet_ntoa(data[26:30]),
                "destination": socket.inet_ntoa(data[30:34]),
                "source_port": struct.unpack("!H", data[34:36])[0],
                "port": struct.unpack("!H", data[36:38])[0],
                "icmp_id": struct.unpack("!H", data[38:40])[0]
                if protocol == 1
                else None,
            }
            break
        description = (
            f"{source}:{source_port} -> {destination}:{port} via {incoming}/{outgoing}"
        )
        if protocol == 1:
            description = f"{source} -> {destination} ICMP type {icmp_type} via {incoming}/{outgoing}"
        assert bool(result) == allowed, (
            f"Unexpected {'forward' if result else 'drop'}: {description}"
        )
        if result:
            assert result["source"] == (translated_source or source), (
                description,
                result,
            )
            assert result["destination"] == (translated_destination or destination), (
                description,
                result,
            )
        print(f"PASS {'allow' if allowed else 'drop'} {description}", flush=True)
        return result


def apply(role, mode):
    path = f"/etc/forcad/{role}/{mode}.nft"
    command("nft", "-c", "-f", path)
    command("nft", "-f", path)
    # Applying a second time must preserve unrelated tables and still succeed.
    command("nft", "-f", path)
    command("nft", "list", "table", "inet", "test_peers")


def router():
    net = Network()
    net.link("wan", "eth0", "203.0.113.10/24", ["203.0.113.1"])
    net.link("vpn", "eth1", "10.0.1.1/30", ["10.0.1.2"])
    net.link("board", "eth2", "10.0.2.1/24", ["10.0.2.10"])
    net.link("a", "eth3.101", "10.101.0.1/24", ["10.101.0.2"])
    net.link("b", "eth3.102", "10.102.0.1/24", ["10.102.0.2"])
    command("ip", "route", "add", "default", "via", "203.0.113.1")
    for prefix in ("10.200.0.0/24", "10.201.0.0/24"):
        command("ip", "route", "add", prefix, "via", "10.0.1.2")
    apply("router", "game")
    player, admin = "10.200.0.2", "10.201.0.2"
    box_a, box_b, gateway = "10.101.0.2", "10.102.0.2", "10.102.0.1"
    # Full TCP handshake verifies reverse NAT and established-flow handling.
    first = net.probe(
        "vpn", "b", player, box_b, source_port=40000, translated_source=gateway
    )
    net.probe(
        "b",
        "vpn",
        box_b,
        gateway,
        port=first["source_port"],
        source_port=8080,
        flags=18,
        sequence=200,
        ack=101,
        translated_destination=player,
    )
    net.probe(
        "vpn",
        "b",
        player,
        box_b,
        source_port=40000,
        flags=16,
        sequence=101,
        ack=201,
        translated_source=gateway,
    )
    net.probe("board", "b", "10.0.2.10", box_b, translated_source=gateway)
    ssh = net.probe(
        "vpn",
        "a",
        player,
        box_a,
        port=22,
        source_port=41000,
        translated_source="10.101.0.1",
    )
    net.probe(
        "a",
        "vpn",
        box_a,
        "10.101.0.1",
        port=ssh["source_port"],
        source_port=22,
        flags=18,
        sequence=200,
        ack=101,
        translated_destination=player,
    )
    net.probe(
        "vpn",
        "a",
        player,
        box_a,
        port=22,
        source_port=41000,
        flags=16,
        sequence=101,
        ack=201,
        translated_source="10.101.0.1",
    )
    net.probe("vpn", "b", player, box_b, port=22, allowed=False)
    net.probe("vpn", "b", admin, box_b, port=22, translated_source=gateway)
    net.probe("vpn", "b", player, box_b, port=9000, udp=True, translated_source=gateway)
    net.probe("vpn", "b", player, box_b, port=9001, udp=True, allowed=False)
    net.probe("vpn", "board", player, "10.0.2.10", port=80)
    net.probe("vpn", "board", player, "10.0.2.10", port=8080, allowed=False)
    net.probe("a", "b", box_a, box_b, allowed=False)
    net.probe("a", "board", box_a, "10.0.2.10", port=80, allowed=False)
    net.probe("a", "vpn", box_a, player, allowed=False)
    net.probe("a", "wan", box_a, "1.1.1.1", port=443, translated_source="203.0.113.10")
    net.probe("a", "wan", box_b, "1.1.1.1", port=443, allowed=False)
    net.probe("a", "board", admin, "10.0.2.10", port=22, allowed=False)
    net.probe("wan", "a", "1.1.1.1", box_a, allowed=False)
    net.probe(
        "wan",
        "vpn",
        "1.1.1.1",
        "203.0.113.10",
        port=56767,
        udp=True,
        translated_destination="10.0.1.2",
    )
    net.probe(
        "wan",
        "vpn",
        "1.1.1.1",
        "203.0.113.10",
        port=56768,
        udp=True,
        translated_destination="10.0.1.2",
    )
    net.probe(
        "wan", "vpn", "1.1.1.1", "203.0.113.10", port=56769, udp=True, allowed=False
    )
    # A tracked timestamp request works in game, but must not pass as a ping
    # after switching to pre-game. This also checks an existing ICMP flow.
    net.probe(
        "vpn",
        "b",
        player,
        box_b,
        icmp_type=13,
        icmp_id=42000,
        translated_source=gateway,
    )
    apply("router", "pre-game")
    net.probe("vpn", "b", player, box_b, allowed=False)
    net.probe(
        "vpn",
        "b",
        player,
        box_b,
        source_port=40000,
        flags=16,
        sequence=101,
        ack=201,
        allowed=False,
    )
    net.probe(
        "b",
        "vpn",
        box_b,
        gateway,
        port=first["source_port"],
        source_port=8080,
        flags=16,
        sequence=201,
        ack=101,
        allowed=False,
    )
    net.probe("vpn", "a", player, box_a, port=22, allowed=False)
    net.probe(
        "vpn",
        "a",
        player,
        box_a,
        port=22,
        source_port=41000,
        flags=16,
        sequence=101,
        ack=201,
        allowed=False,
    )
    net.probe(
        "a",
        "vpn",
        box_a,
        "10.101.0.1",
        port=ssh["source_port"],
        source_port=22,
        flags=16,
        sequence=201,
        ack=101,
        allowed=False,
    )
    net.probe("vpn", "a", player, box_a, allowed=False)
    net.probe("vpn", "b", player, box_b, port=22, allowed=False)
    net.probe("vpn", "b", player, box_b, port=9000, udp=True, allowed=False)
    net.probe("vpn", "b", player, box_b, icmp_type=13, icmp_id=42000, allowed=False)
    net.probe("vpn", "a", player, box_a, icmp_type=13, allowed=False)
    for link, box, box_gateway in (("a", box_a, "10.101.0.1"), ("b", box_b, gateway)):
        ping = net.probe(
            "vpn", link, player, box, icmp_type=8, translated_source=box_gateway
        )
        net.probe(
            link,
            "vpn",
            box,
            box_gateway,
            icmp_type=0,
            icmp_id=ping["icmp_id"],
            translated_destination=player,
        )
    net.probe("board", "b", "10.0.2.10", box_b, translated_source=gateway)
    net.probe("vpn", "b", admin, box_b, port=22, translated_source=gateway)
    net.probe("vpn", "a", admin, box_a, port=4444, translated_source="10.101.0.1")
    net.probe("vpn", "b", admin, box_b, port=9001, udp=True, translated_source=gateway)
    net.probe("vpn", "b", admin, box_b, icmp_type=13, translated_source=gateway)
    net.probe("vpn", "board", player, "10.0.2.10", port=80)
    apply("router", "game")
    net.probe("vpn", "b", player, box_b, translated_source=gateway)
    net.probe("vpn", "a", player, box_a, port=22, translated_source="10.101.0.1")


def vpn():
    net = Network()
    net.link("transit", "eth0", "10.0.1.2/30", ["10.0.1.1"])
    net.link(
        "players",
        "wg-attack",
        "10.200.0.1/24",
        ["10.200.0.2", "10.200.0.3", "10.200.0.9"],
    )
    net.link("admins", "wg-admin", "10.201.0.1/24", ["10.201.0.2"])
    command("ip", "route", "add", "default", "via", "10.0.1.1")
    apply("vpn", "firewall")
    player, admin, box = "10.200.0.2", "10.201.0.2", "10.102.0.2"
    net.probe("players", "transit", player, box, source_port=40000)
    net.probe(
        "transit",
        "players",
        box,
        player,
        port=40000,
        source_port=8080,
        flags=18,
        sequence=200,
        ack=101,
    )
    net.probe("players", "players", player, "10.200.0.3")
    net.probe("players", "players", player, "10.200.0.9", allowed=False)
    net.probe("players", "admins", player, admin, allowed=False)
    net.probe("players", "transit", admin, box, allowed=False)
    net.probe("transit", "players", box, player, port=22, allowed=False)
    net.probe("admins", "transit", admin, box, port=22)
    net.probe("admins", "players", admin, player, port=22, source_port=41000)
    net.probe(
        "players",
        "admins",
        player,
        admin,
        port=41000,
        source_port=22,
        flags=18,
        sequence=200,
        ack=101,
    )


if __name__ == "__main__":
    print(command("pwd", "-P").strip())
    role = sys.argv[1]
    command("ifquery", "--interfaces", f"/etc/forcad/{role}/interfaces", "--list")
    {"router": router, "vpn": vpn}[role]()

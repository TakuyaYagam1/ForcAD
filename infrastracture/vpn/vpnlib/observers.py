"""Private VPN packet and authenticated endpoint observers."""

import argparse
import fcntl
import ipaddress
import json
import logging
import os
import pwd
import re
import resource
import shutil
import sqlite3
import stat
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

INVENTORY_FILE = "/etc/forcad/vpn/observer-inventory.json"
VENUE_CIDRS_FILE = "/etc/forcad/vpn/endpoint-venues.txt"
CAPTURE_DIRECTORY = Path("/var/lib/forcad-vpn-pcap")
ENDPOINT_DATABASE = Path("/var/lib/forcad-wg-endpoints/history.sqlite3")
OBSERVER_SCRIPT = "/usr/lib/forcad/vpn/vpnlib/observers.py"
HISTORY_SCRIPT = "/usr/lib/forcad/vpn/vpnlib/history.py"

CAPTURE_INTERVAL_SECONDS = 300
CAPTURE_FILE_KB = 4_000_000
RING_CAPTURE_FILES = 96
MAX_CAPTURE_FILES = 2 * RING_CAPTURE_FILES
CAPTURE_SNAPLEN = 262_144
CAPTURE_FILE_LIMIT_BYTES = CAPTURE_FILE_KB * 1024 + CAPTURE_SNAPLEN + 40
CAPTURE_STORAGE_LIMIT_BYTES = MAX_CAPTURE_FILES * CAPTURE_FILE_LIMIT_BYTES
CAPTURE_RESERVE_BYTES = 10 * 1024**3
MAX_EXPORT_WINDOW_MS = 6 * 60 * 60 * 1000
MAX_EXPORT_FILES = 8
MAX_EXPORT_BYTES = 512 * 1024 * 1024
MAX_EXPORT_FILE_BYTES = 64 * 1024 * 1024
MAX_HISTORY_AGE_SECONDS = 30 * 24 * 60 * 60
MAX_HISTORY_ROWS = 100_000
MAX_HISTORY_DATABASE_BYTES = 64 * 1024 * 1024
ENDPOINT_POLL_SECONDS = 5
MAX_DISSECTION_PACKETS = 100_000
MAX_HANDSHAKE_AGE_SECONDS = 180
BASELINE_REFRESH_SECONDS = 24 * 60 * 60

_CAPTURE_NAME = re.compile(r"^capture_[0-9]+_[0-9]{5}_[0-9]{14}\.pcap$")
_EXPORT_NAME = re.compile(r"^flow-[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.pcap$")
_DROP_REPORT = re.compile(
    r"Packets received/dropped on interface 'wg-attack': "
    r"(?P<received>\d+)/(?P<dropped>\d+) "
    r"\(pcap:(?P<pcap>\d+)/dumpcap:(?P<dumpcap>\d+)/"
    r"flushed:(?P<flushed>\d+)/ps_ifdrop:(?P<interface>\d+)\)"
)

PCAP_SERVICE = f"""[Unit]
Description=ForcAD VPN packet capture
After=wg-quick@wg-attack.service
ConditionPathExists=/sys/class/net/wg-attack
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=simple
User=root
Group=root
UMask=0077
Environment=TZ=UTC
Environment=LC_ALL=C
StateDirectory=forcad-vpn-pcap
StateDirectoryMode=0700
ExecStart=/usr/bin/python3 {OBSERVER_SCRIPT} pcap-capture {INVENTORY_FILE} {CAPTURE_DIRECTORY}
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_NETLINK AF_PACKET
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
StandardOutput=journal
StandardError=journal
"""

ENDPOINTS_SERVICE = f"""[Unit]
Description=ForcAD authenticated WireGuard endpoint history
After=wg-quick@wg-attack.service forcad-wg-history.socket
Wants=forcad-wg-history.socket
ConditionPathExists=/sys/class/net/wg-attack
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=simple
User=root
Group=root
UMask=0077
StateDirectory=forcad-wg-endpoints
StateDirectoryMode=0700
ExecStart=/usr/bin/python3 {OBSERVER_SCRIPT} endpoints {INVENTORY_FILE} {VENUE_CIDRS_FILE} {ENDPOINT_DATABASE}
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_NETLINK AF_INET AF_INET6
CapabilityBoundingSet=CAP_NET_ADMIN
AmbientCapabilities=CAP_NET_ADMIN
ReadWritePaths=/var/lib/forcad-wg-endpoints
StandardOutput=null
StandardError=journal
"""

HISTORY_SOCKET = """[Unit]
Description=ForcAD WireGuard endpoint history socket
PartOf=forcad-wg-endpoints.service

[Socket]
ListenStream=/run/forcad-wg-history.sock
SocketUser=root
SocketGroup=adm
SocketMode=0660
RemoveOnStop=yes
"""

HISTORY_SERVICE = f"""[Unit]
Description=ForcAD WireGuard endpoint history viewer
PartOf=forcad-wg-endpoints.service

[Service]
Type=simple
User=root
Group=adm
UMask=0077
ExecStart=/usr/bin/python3 {HISTORY_SCRIPT} {ENDPOINT_DATABASE}
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateNetwork=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX
ReadOnlyPaths=/var/lib/forcad-wg-endpoints
StandardOutput=null
StandardError=journal
"""



def normalize_inventory(teams):
    """Validate and reduce bundle inventory to the fields observers need."""
    if not isinstance(teams, list) or not teams:
        raise ValueError("VPN observer inventory is empty")
    result = []
    seen_peers = set()
    seen_vulnboxes = set()
    for team in teams:
        if not isinstance(team, dict):
            raise ValueError("Invalid VPN observer inventory")
        name = team.get("name")
        if not isinstance(name, str) or not name or any(ord(char) < 0x20 for char in name):
            raise ValueError("Invalid VPN observer inventory")
        vulnbox = _ipv4(team.get("vulnbox"))
        if vulnbox in seen_vulnboxes:
            raise ValueError("Duplicate vulnbox in VPN observer inventory")
        seen_vulnboxes.add(vulnbox)
        peers = team.get("peers")
        if not isinstance(peers, list):
            raise ValueError("Invalid VPN observer inventory")
        normalized_peers = []
        for peer in peers:
            address = _ipv4(peer)
            if address in seen_peers:
                raise ValueError("Duplicate peer in VPN observer inventory")
            seen_peers.add(address)
            normalized_peers.append(address)
        result.append({"name": name, "vulnbox": vulnbox, "peers": normalized_peers})
    return result


def inventory_document(teams):
    return json.dumps({"teams": normalize_inventory(teams)}, indent=2) + "\n"


def _ipv4(value):
    try:
        address = ipaddress.ip_address(value)
    except (TypeError, ValueError):
        raise ValueError("VPN observer inventory must contain IPv4 addresses") from None
    if address.version != 4:
        raise ValueError("VPN observer inventory must contain IPv4 addresses")
    return str(address)


def build_capture_filter(teams):
    """Build a bidirectional BPF filter from exact peer and vulnbox addresses."""
    inventory = normalize_inventory(teams)
    peers = sorted({peer for team in inventory for peer in team["peers"]}, key=ipaddress.IPv4Address)
    vulnboxes = sorted({team["vulnbox"] for team in inventory}, key=ipaddress.IPv4Address)
    if not peers or not vulnboxes:
        raise ValueError("VPN observer inventory has no participant peers")
    peer_sources = " or ".join(f"src host {address}" for address in peers)
    peer_destinations = " or ".join(f"dst host {address}" for address in peers)
    vulnbox_sources = " or ".join(f"src host {address}" for address in vulnboxes)
    vulnbox_destinations = " or ".join(f"dst host {address}" for address in vulnboxes)
    return (
        f"(({peer_sources}) and ({vulnbox_destinations})) or "
        f"(({vulnbox_sources}) and ({peer_destinations}))"
    )


def _endpoint_address(value):
    if value.startswith("["):
        closing = value.find("]")
        if closing < 0 or value[closing + 1 : closing + 2] != ":":
            return None
        host, port_text = value[1:closing], value[closing + 2 :]
    else:
        host, separator, port_text = value.rpartition(":")
        if not separator:
            return None
    if not port_text.isdecimal():
        return None
    port = int(port_text)
    if not 1 <= port <= 65535:
        return None
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        return None
    if address.is_unspecified or address.is_multicast:
        return None
    return str(address)


def _wireguard_rows(output):
    rows = {}
    duplicates = set()
    for line in output.splitlines():
        columns = line.split()
        if len(columns) != 2:
            continue
        key, value = columns
        if key in rows:
            duplicates.add(key)
        else:
            rows[key] = value
    for key in duplicates:
        rows.pop(key, None)
    return rows


def parse_venue_cidrs(lines):
    networks = []
    try:
        for line in lines:
            value = line.split("#", 1)[0].strip()
            if not value:
                continue
            networks.append(ipaddress.ip_network(value, strict=False))
    except (TypeError, ValueError):
        return None
    return tuple(networks) if networks else None


def venue_for(external_ip, networks):
    if not networks:
        return "unknown"
    address = ipaddress.ip_address(external_ip)
    return "venue" if any(address.version == network.version and address in network for network in networks) else "outside"


def parse_wireguard_snapshot(
    teams, allowed_ips, endpoints, handshakes, venue_networks=None, observed_at=None
):
    """Return recent authenticated endpoints mapped only to inventory /32 peers."""
    if observed_at is None:
        observed_at = int(time.time())
    if isinstance(observed_at, bool) or not isinstance(observed_at, int) or observed_at < 0:
        raise ValueError("Invalid observation time")
    inventory = normalize_inventory(teams)
    member_by_ip = {
        peer: (team["name"], f"member-{member:02d}")
        for team in inventory
        for member, peer in enumerate(team["peers"], 1)
    }
    key_by_source = {}
    ambiguous_sources = set()
    for key, value in _wireguard_rows(allowed_ips).items():
        entries = value.split(",")
        if len(entries) != 1:
            continue
        try:
            allowed = ipaddress.ip_network(entries[0], strict=True)
        except ValueError:
            continue
        if allowed.version != 4 or allowed.prefixlen != 32:
            continue
        source_ip = str(allowed.network_address)
        if source_ip not in member_by_ip:
            continue
        if source_ip in key_by_source:
            ambiguous_sources.add(source_ip)
        else:
            key_by_source[source_ip] = key
    for source_ip in ambiguous_sources:
        key_by_source.pop(source_ip, None)

    endpoint_by_key = {}
    for key, value in _wireguard_rows(endpoints).items():
        address = _endpoint_address(value)
        if address is not None:
            endpoint_by_key[key] = address
    handshake_by_key = {}
    for key, value in _wireguard_rows(handshakes).items():
        try:
            handshake_by_key[key] = int(value)
        except ValueError:
            continue

    current = {}
    for source_ip, key in key_by_source.items():
        external_ip = endpoint_by_key.get(key)
        last_handshake = handshake_by_key.get(key, 0)
        handshake_age = observed_at - last_handshake
        if (
            external_ip is None
            or last_handshake <= 0
            or not 0 <= handshake_age <= MAX_HANDSHAKE_AGE_SECONDS
        ):
            continue
        team_name, member = member_by_ip[source_ip]
        current[source_ip] = {
            "team_name": team_name,
            "member": member,
            "external_ip": external_ip,
            "venue": venue_for(external_ip, venue_networks),
            "authenticated_recently": True,
        }
    return current


def read_venue_cidrs(path=VENUE_CIDRS_FILE):
    try:
        text = Path(path).read_text(encoding="utf-8")
    except FileNotFoundError:
        return None
    except OSError:
        return None
    return parse_venue_cidrs(text.splitlines())


def create_history(connection):
    connection.execute(
        "CREATE TABLE IF NOT EXISTS observations ("
        "id INTEGER PRIMARY KEY, observed_at INTEGER NOT NULL, source_ip TEXT NOT NULL, "
        "team_name TEXT NOT NULL, member TEXT NOT NULL, external_ip TEXT NOT NULL, "
        "venue TEXT NOT NULL CHECK (venue IN ('venue', 'outside', 'unknown')), "
        "kind TEXT NOT NULL CHECK (kind IN ('baseline', 'first', 'change')))"
    )
    connection.execute(
        "CREATE INDEX IF NOT EXISTS observations_time_source "
        "ON observations(observed_at, source_ip)"
    )
    connection.execute(
        "CREATE INDEX IF NOT EXISTS observations_source_time "
        "ON observations(source_ip, observed_at)"
    )
    connection.commit()


def prune_history(connection, observed_at):
    connection.execute(
        "DELETE FROM observations WHERE observed_at < ?",
        (observed_at - MAX_HISTORY_AGE_SECONDS,),
    )
    connection.execute(
        "DELETE FROM observations WHERE id IN ("
        "SELECT id FROM observations ORDER BY observed_at DESC, id DESC "
        "LIMIT -1 OFFSET ?)" ,
        (MAX_HISTORY_ROWS,),
    )


def record_observations(connection, previous, current, *, bootstrap, observed_at):
    """Persist run baselines and authenticated address transitions."""
    if isinstance(observed_at, bool) or not isinstance(observed_at, int) or observed_at < 0:
        raise ValueError("Invalid observation time")
    state = dict(previous)
    rows = []
    for source_ip, item in current.items():
        source_ip = _ipv4(source_ip)
        external_ip = str(ipaddress.ip_address(item["external_ip"]))
        previous_ip = previous.get(source_ip)
        if bootstrap:
            kind = "baseline"
        elif previous_ip is None:
            kind = "first"
        elif previous_ip != external_ip:
            kind = "change"
        else:
            kind = None
            if item.get("authenticated_recently"):
                latest = connection.execute(
                    "SELECT MAX(observed_at) FROM observations WHERE source_ip = ?",
                    (source_ip,),
                ).fetchone()[0]
                if latest is None or observed_at - latest >= BASELINE_REFRESH_SECONDS:
                    kind = "baseline"
        if kind:
            rows.append(
                (
                    observed_at,
                    source_ip,
                    item["team_name"],
                    item["member"],
                    external_ip,
                    item["venue"],
                    kind,
                )
            )
        state[source_ip] = external_ip
    if rows:
        connection.executemany(
            "INSERT INTO observations "
            "(observed_at, source_ip, team_name, member, external_ip, venue, kind) "
            "VALUES (?, ?, ?, ?, ?, ?, ?)",
            rows,
        )
    prune_history(connection, observed_at)
    connection.commit()
    return state


def _private_directory(path):
    path = Path(path)
    if path.is_symlink():
        raise ValueError("Observer storage directory must not be a symlink")
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.stat(follow_symlinks=False)
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid():
        raise ValueError("Observer storage directory is not privately owned")
    path.chmod(0o700)
    return path


def capture_files(directory):
    directory = Path(directory)
    if not directory.exists() or directory.is_symlink():
        return []
    files = []
    for path in directory.iterdir():
        if not _CAPTURE_NAME.fullmatch(path.name):
            continue
        try:
            info = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid():
            files.append(path)
    return files


def prune_capture_files(directory, keep=MAX_CAPTURE_FILES - RING_CAPTURE_FILES):
    """Keep restart leftovers below the global ring-buffer file budget."""
    if keep < 0 or keep > MAX_CAPTURE_FILES:
        raise ValueError("Invalid capture retention limit")
    files = capture_files(directory)
    files.sort(key=lambda path: (path.stat().st_mtime_ns, path.name))
    removed = 0
    for path in files[: max(0, len(files) - keep)]:
        path.unlink()
        removed += 1
    return removed


def _capture_timestamp(path):
    match = _CAPTURE_NAME.fullmatch(Path(path).name)
    if not match:
        return None
    timestamp = Path(path).name.rsplit("_", 1)[1][:-5]
    try:
        return int(
            datetime.strptime(timestamp, "%Y%m%d%H%M%S")
            .replace(tzinfo=timezone.utc)
            .timestamp()
            * 1000
        )
    except ValueError:
        return None


def _validate_window(from_ms, to_ms):
    if (
        isinstance(from_ms, bool)
        or isinstance(to_ms, bool)
        or not isinstance(from_ms, int)
        or not isinstance(to_ms, int)
        or from_ms < 0
        or to_ms < from_ms
        or to_ms - from_ms > MAX_EXPORT_WINDOW_MS
    ):
        raise ValueError("Invalid or excessive observer time range")


def select_capture_files(directory, from_ms, to_ms):
    """Select time-range segments and the preceding second group for boundary packets."""
    _validate_window(from_ms, to_ms)
    timed = [
        (path, timestamp)
        for path in capture_files(directory)
        if (timestamp := _capture_timestamp(path)) is not None
    ]
    timed.sort(key=lambda item: (item[1], item[0].name))
    start_second = from_ms // 1000
    end_second = to_ms // 1000
    preceding_seconds = [
        timestamp // 1000
        for _, timestamp in timed
        if timestamp // 1000 < start_second
    ]
    selected_seconds = set(range(start_second, end_second + 1))
    if preceding_seconds:
        selected_seconds.add(max(preceding_seconds))
    return [
        path
        for path, timestamp in timed
        if timestamp // 1000 in selected_seconds
    ]


def _epoch_seconds(milliseconds):
    return f"{milliseconds // 1000}.{milliseconds % 1000:03d}"


def build_flow_filter(protocol, source_ip, source_port, destination_ip, destination_port, from_ms, to_ms):
    """Build a bidirectional display filter only from validated 5-tuple fields."""
    _validate_window(from_ms, to_ms)
    if not isinstance(protocol, str) or protocol.lower() not in {"tcp", "udp"}:
        raise ValueError("Observer exports support only TCP and UDP flows")
    protocol = protocol.lower()
    try:
        source = ipaddress.ip_address(source_ip)
        destination = ipaddress.ip_address(destination_ip)
    except (TypeError, ValueError):
        raise ValueError("Invalid observer flow address") from None
    if source.version != destination.version:
        raise ValueError("Observer flow addresses must use the same IP version")
    for port in (source_port, destination_port):
        if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
            raise ValueError("Invalid observer flow port")
    field = "ip" if source.version == 4 else "ipv6"
    forward = (
        f"({field}.src == {source} and {field}.dst == {destination} and "
        f"{protocol}.srcport == {source_port} and {protocol}.dstport == {destination_port})"
    )
    reverse = (
        f"({field}.src == {destination} and {field}.dst == {source} and "
        f"{protocol}.srcport == {destination_port} and {protocol}.dstport == {source_port})"
    )
    return (
        f"(({forward}) or ({reverse})) and "
        f"frame.time_epoch >= {_epoch_seconds(from_ms)} and "
        f"frame.time_epoch <= {_epoch_seconds(to_ms)}"
    )


def _managed_exports(directory):
    if not directory.exists() or directory.is_symlink():
        return []
    files = []
    for path in directory.iterdir():
        if not _EXPORT_NAME.fullmatch(path.name):
            continue
        try:
            info = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISREG(info.st_mode):
            files.append(path)
    return files


def _export_child_setup():
    resource.setrlimit(
        resource.RLIMIT_FSIZE,
        (MAX_EXPORT_FILE_BYTES, MAX_EXPORT_FILE_BYTES),
    )
    if os.geteuid() == 0:
        account = pwd.getpwnam("nobody")
        os.setgroups([])
        os.setgid(account.pw_gid)
        os.setuid(account.pw_uid)


def _stop_process(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def extract_flow(
    capture_directory,
    protocol,
    source_ip,
    source_port,
    destination_ip,
    destination_port,
    from_ms,
    to_ms,
    name,
    *,
    mergecap="/usr/bin/mergecap",
    tshark="/usr/bin/tshark",
):
    """Write a bounded operator-only flow export from matching adjacent captures."""
    expression = build_flow_filter(
        protocol, source_ip, source_port, destination_ip, destination_port, from_ms, to_ms
    )
    if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}", name):
        raise ValueError("Invalid flow export name")
    capture_directory = Path(capture_directory)
    if capture_directory.is_symlink() or not capture_directory.is_dir():
        raise ValueError("Observer: no retained capture segments overlap this time range")
    directory_info = capture_directory.stat(follow_symlinks=False)
    if (
        not stat.S_ISDIR(directory_info.st_mode)
        or directory_info.st_uid != os.geteuid()
        or stat.S_IMODE(directory_info.st_mode) & 0o077
    ):
        raise ValueError("Observer: capture storage directory is not private")
    files = select_capture_files(capture_directory, from_ms, to_ms)
    if not files:
        raise ValueError("Observer: no retained capture segments overlap this time range")
    if len(files) > MAX_CAPTURE_FILES:
        raise ValueError("Observer: selected capture segments exceed the bounded source limit")
    if not Path(mergecap).is_file() or not Path(tshark).is_file():
        raise OSError("Flow export tools are unavailable")
    if shutil.disk_usage(capture_directory).free < MAX_EXPORT_FILE_BYTES + CAPTURE_RESERVE_BYTES:
        raise ValueError(
            "Observer: insufficient free space for this export and 10 GiB filesystem reserve"
        )

    exports = _private_directory(capture_directory / "exports")
    lock_path = exports / ".extract.lock"
    lock_path.touch(mode=0o600, exist_ok=True)
    lock_path.chmod(0o600)
    with lock_path.open("rb") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        for partial in exports.glob(".partial-*.pcap"):
            try:
                if stat.S_ISREG(partial.lstat().st_mode):
                    partial.unlink()
            except FileNotFoundError:
                pass
        existing = _managed_exports(exports)
        final_path = exports / f"flow-{name}.pcap"
        if final_path.exists() or final_path.is_symlink():
            raise ValueError("Observer: flow export name already exists")
        if len(existing) >= MAX_EXPORT_FILES:
            raise ValueError("Observer: flow export retention limit reached")
        used = sum(path.stat().st_size for path in existing)
        if used + MAX_EXPORT_FILE_BYTES > MAX_EXPORT_BYTES:
            raise ValueError("Observer: flow export storage limit reached")
        descriptor, partial_name = tempfile.mkstemp(
            prefix=".partial-", suffix=".pcap", dir=exports
        )
        partial_path = Path(partial_name)
        merge = None
        tshark_process = None
        output = None
        try:
            merge = subprocess.Popen(
                [mergecap, "-w", "-", *(str(path) for path in files)],
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                close_fds=True,
            )
            if merge.stdout is None:
                raise OSError("Capture merge could not be started")
            output = os.fdopen(descriptor, "wb", buffering=0)
            descriptor = None
            tshark_process = subprocess.Popen(
                [
                    tshark,
                    "-n",
                    "-M",
                    str(MAX_DISSECTION_PACKETS),
                    "-r",
                    "-",
                    "-Y",
                    expression,
                    "-F",
                    "pcap",
                    "-w",
                    "-",
                ],
                stdin=merge.stdout,
                stdout=output,
                stderr=subprocess.DEVNULL,
                close_fds=True,
                cwd="/",
                env={
                    "HOME": "/nonexistent",
                    "LC_ALL": "C",
                    "PATH": "/usr/bin:/bin",
                    "TZ": "UTC",
                },
                preexec_fn=_export_child_setup,
            )
            merge.stdout.close()
            tshark_status = tshark_process.wait()
            merge_status = merge.wait()
            output.close()
            output = None
            size = partial_path.stat().st_size
            if tshark_status != 0 or merge_status != 0:
                if size >= MAX_EXPORT_FILE_BYTES:
                    raise ValueError("Observer: flow export reached its 64 MiB file limit")
                raise OSError("Flow export failed")
            if size <= 24:
                raise ValueError(
                    "Observer: no packets matched the requested flow and time range"
                )
            if size > MAX_EXPORT_FILE_BYTES:
                raise ValueError("Observer: flow export exceeded its per-file storage limit")
            os.chmod(partial_path, 0o600)
            partial_path.replace(final_path)
        except BaseException:
            if tshark_process is not None:
                _stop_process(tshark_process)
            if merge is not None:
                if merge.stdout is not None:
                    merge.stdout.close()
                _stop_process(merge)
            if output is not None:
                output.close()
            elif descriptor is not None:
                os.close(descriptor)
            partial_path.unlink(missing_ok=True)
            raise
    return final_path


def parse_drop_summary(lines):
    """Return the latest final dumpcap counters without exposing capture details."""
    for line in reversed(list(lines)):
        match = _DROP_REPORT.search(line)
        if match:
            return {name: int(value) for name, value in match.groupdict().items()}
    return None


def _last_drop_report():
    result = subprocess.run(
        [
            "/usr/bin/journalctl",
            "--unit=forcad-vpn-pcap.service",
            "--output=cat",
            "--no-pager",
            "--lines=100",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        return None
    return parse_drop_summary(result.stdout.splitlines())


def _service_active():
    result = subprocess.run(
        ["/usr/bin/systemctl", "is-active", "--quiet", "forcad-vpn-pcap.service"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    )
    return result.returncode == 0


def _human_bytes(value):
    units = ("B", "KiB", "MiB", "GiB", "TiB")
    amount = float(value)
    for unit in units:
        if amount < 1024 or unit == units[-1]:
            return f"{amount:.1f} {unit}"
        amount /= 1024


def pcap_status(directory=CAPTURE_DIRECTORY):
    directory = Path(directory)
    files = capture_files(directory)
    used = sum(path.stat().st_size for path in files)
    if directory.exists() and not directory.is_symlink():
        free = shutil.disk_usage(directory).free
    else:
        free = shutil.disk_usage(directory.parent).free
    print(
        f"Capture storage: {_human_bytes(used)} used; "
        f"{len(files)}/{MAX_CAPTURE_FILES} files; "
        f"budget <= {_human_bytes(CAPTURE_STORAGE_LIMIT_BYTES)}; "
        f"filesystem free {_human_bytes(free)}"
    )
    print(
        "Start free-space requirement: "
        f"{_human_bytes(RING_CAPTURE_FILES * CAPTURE_FILE_LIMIT_BYTES)} ring + "
        f"{_human_bytes(MAX_EXPORT_BYTES)} exports + "
        f"{_human_bytes(CAPTURE_RESERVE_BYTES)} reserve"
    )
    timestamps = [(_capture_timestamp(path), path.name) for path in files]
    timestamps = [(stamp, name) for stamp, name in timestamps if stamp is not None]
    if timestamps:
        oldest, name = min(timestamps)
        instant = datetime.fromtimestamp(oldest / 1000, timezone.utc).isoformat()
        print(f"Oldest segment: {instant} ({name})")
    else:
        print("Oldest segment: none retained")
    if _service_active():
        print("Current capture drops: not finalized while capture is running")
    else:
        print("Current capture drops: service not running")
    drops = _last_drop_report()
    if drops is None:
        print("Last completed capture drops: unavailable")
    else:
        print(
            "Last completed capture drops: "
            f"received={drops['received']} dropped={drops['dropped']} "
            f"(pcap={drops['pcap']}, dumpcap={drops['dumpcap']}, "
            f"flushed={drops['flushed']}, interface={drops['interface']})"
        )


def load_inventory(path):
    value = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(value, dict) or not isinstance(value.get("teams"), list):
        raise ValueError("Invalid observer inventory")
    return normalize_inventory(value["teams"])


def _capture_preflight(directory):
    required = (
        RING_CAPTURE_FILES * CAPTURE_FILE_LIMIT_BYTES
        + CAPTURE_RESERVE_BYTES
        + MAX_EXPORT_BYTES
    )
    free = shutil.disk_usage(directory).free
    if free < required:
        raise ValueError(
            "Observer: insufficient free space for the full capture ring, "
            "export allowance, and 10 GiB filesystem reserve"
        )


def pcap_capture(inventory_path, directory):
    teams = load_inventory(inventory_path)
    capture_filter = build_capture_filter(teams)
    directory = _private_directory(directory)
    _capture_preflight(directory)
    prune_capture_files(directory)
    used_prefixes = {path.name.split("_", 2)[1] for path in capture_files(directory)}
    run_id = time.time_ns()
    while str(run_id) in used_prefixes:
        run_id += 1
    output = directory / f"capture_{run_id}.pcap"
    command = [
        "/usr/bin/dumpcap",
        "-i",
        "wg-attack",
        "-p",
        "-B",
        "8",
        "-s",
        str(CAPTURE_SNAPLEN),
        "-f",
        capture_filter,
        "-F",
        "pcap",
        "-w",
        str(output),
        "-b",
        f"interval:{CAPTURE_INTERVAL_SECONDS}",
        "-b",
        f"filesize:{CAPTURE_FILE_KB}",
        "-b",
        f"files:{RING_CAPTURE_FILES}",
        "-q",
    ]
    os.execv(command[0], command)


def read_wireguard(teams, venue_networks, observed_at=None):
    allowed = subprocess.run(
        ["/usr/bin/wg", "show", "wg-attack", "allowed-ips"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    endpoints = subprocess.run(
        ["/usr/bin/wg", "show", "wg-attack", "endpoints"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    handshakes = subprocess.run(
        ["/usr/bin/wg", "show", "wg-attack", "latest-handshakes"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    return parse_wireguard_snapshot(
        teams, allowed, endpoints, handshakes, venue_networks, observed_at
    )

def open_history(path):
    path = Path(path)
    _private_directory(path.parent)
    if path.is_symlink():
        raise ValueError("Endpoint history database must not be a symlink")
    if path.exists() and not stat.S_ISREG(path.lstat().st_mode):
        raise ValueError("Endpoint history database must be a regular file")
    connection = sqlite3.connect(path, timeout=5)
    path.chmod(0o600)
    connection.execute("PRAGMA busy_timeout=5000")
    connection.execute("PRAGMA secure_delete=ON")
    page_size = connection.execute("PRAGMA page_size").fetchone()[0]
    connection.execute(
        f"PRAGMA max_page_count={MAX_HISTORY_DATABASE_BYTES // page_size}"
    )
    create_history(connection)
    return connection




def observe_endpoints(inventory_path, venues_path, database_path):
    teams = load_inventory(inventory_path)
    connection = open_history(database_path)
    state = {}
    bootstrap = True
    last_error = 0.0
    try:
        while True:
            started = time.monotonic()
            try:
                observed_at = int(time.time())
                current = read_wireguard(
                    teams,
                    read_venue_cidrs(venues_path),
                    observed_at=observed_at,
                )
                state = record_observations(
                    connection,
                    state,
                    current,
                    bootstrap=bootstrap,
                    observed_at=observed_at,
                )
                bootstrap = False
                last_error = 0.0
            except (OSError, ValueError, sqlite3.Error, subprocess.SubprocessError):
                now = time.monotonic()
                if now - last_error >= 60:
                    logging.error("WireGuard endpoint sample unavailable")
                    last_error = now
            time.sleep(max(0, ENDPOINT_POLL_SECONDS - (time.monotonic() - started)))
    finally:
        connection.close()


def _add_flow_args(parser):
    parser.add_argument("--protocol", required=True, choices=("tcp", "udp"))
    parser.add_argument("--source", required=True)
    parser.add_argument("--source-port", required=True, type=int)
    parser.add_argument("--destination", required=True)
    parser.add_argument("--destination-port", required=True, type=int)
    parser.add_argument("--from-ms", required=True, type=int)
    parser.add_argument("--to-ms", required=True, type=int)
    parser.add_argument("--name", required=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    capture = commands.add_parser("pcap-capture")
    capture.add_argument("inventory")
    capture.add_argument("directory")
    endpoints = commands.add_parser("endpoints")
    endpoints.add_argument("inventory")
    endpoints.add_argument("venues")
    endpoints.add_argument("database")
    status = commands.add_parser("pcap-status")
    status.add_argument("--directory", default=str(CAPTURE_DIRECTORY))
    extract = commands.add_parser("pcap-extract")
    extract.add_argument("--directory", default=str(CAPTURE_DIRECTORY))
    _add_flow_args(extract)
    arguments = parser.parse_args(argv)
    try:
        if arguments.command == "pcap-capture":
            pcap_capture(arguments.inventory, arguments.directory)
        elif arguments.command == "endpoints":
            observe_endpoints(arguments.inventory, arguments.venues, arguments.database)
        elif arguments.command == "pcap-status":
            if os.geteuid() != 0:
                raise ValueError("Observer: operator privileges are required")
            pcap_status(arguments.directory)
        elif arguments.command == "pcap-extract":
            if os.geteuid() != 0:
                raise ValueError("Observer: operator privileges are required")
            output = extract_flow(
                arguments.directory,
                arguments.protocol,
                arguments.source,
                arguments.source_port,
                arguments.destination,
                arguments.destination_port,
                arguments.from_ms,
                arguments.to_ms,
                arguments.name,
            )
            print(f"Flow export created: {output.name}")
    except (OSError, ValueError, sqlite3.Error, subprocess.SubprocessError) as error:
        message = (
            str(error)
            if isinstance(error, ValueError)
            and (
                str(error).startswith("Invalid")
                or str(error).startswith("Observer:")
            )
            else "Observer operation failed"
        )
        print(message, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

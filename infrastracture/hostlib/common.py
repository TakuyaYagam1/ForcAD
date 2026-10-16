"""Shared host bundle file and configuration helpers."""

import base64
import ipaddress
import re
import subprocess
from pathlib import Path


def run(*args):
    result = subprocess.run(
        args, capture_output=True, text=True, check=False, timeout=30
    )
    if result.returncode:
        raise ValueError(f"Command failed: {' '.join(map(str, args))}")
    return result.stdout


def regular(path):
    path = Path(path)
    for parent in (path, *path.parents):
        if parent.is_symlink():
            raise ValueError(f"Refusing symlink: {parent}")
    if path.exists() and not path.is_file():
        raise ValueError(f"Not a regular file: {path}")
    return path


def read_yaml(path):
    try:
        import yaml
    except ImportError as error:
        raise ValueError("Install python3-yaml on the ForcAD host") from error
    try:
        return yaml.safe_load(regular(path).read_text())
    except yaml.YAMLError as error:
        raise ValueError(f"Invalid YAML: {path}") from error


def dump_yaml(value):
    import yaml

    return yaml.safe_dump(value, allow_unicode=True, sort_keys=False).encode()


def merge_env(original, updates):
    lines = original.splitlines()
    for key, value in updates.items():
        matches = [
            i
            for i, line in enumerate(lines)
            if re.match(rf"\s*(?:export\s+)?{re.escape(key)}\s*=", line)
        ]
        if len(matches) > 1:
            raise ValueError(f"Duplicate environment setting: {key}")
        if matches:
            lines[matches[0]] = f"{key}={value}"
        else:
            lines.append(f"{key}={value}")
    return ("\n".join(lines) + "\n").encode()


def merge_targets(groups, target):
    if not isinstance(groups, list) or any(
        not isinstance(group, dict) or not isinstance(group.get("targets"), list)
        for group in groups
    ):
        raise ValueError("Monitoring discovery must contain a list of target groups")
    if not any(target in group["targets"] for group in groups):
        groups.append({"targets": [target]})
    return groups


def merge_teams(document, expected):
    current = document.get("teams", []) if isinstance(document, dict) else []
    ips = [team.get("ip") for team in current if isinstance(team, dict)]
    wanted = {team["vulnbox"]: team for team in expected}
    if (
        any(not isinstance(ip, str) for ip in ips)
        or len(ips) != len(current)
        or len(set(ips)) != len(ips)
        or set(ips) != set(wanted)
    ):
        raise ValueError(
            "config.yml team IPs must match this bundle. Set the IPs explicitly; names and tokens are never replaced by position."
        )
    for team in current:
        cidrs = team.setdefault("traffic_cidrs", [])
        if not isinstance(cidrs, list):
            raise TypeError("traffic_cidrs must be a list")
        for cidr in (team["ip"] + "/32", wanted[team["ip"]]["vpn_cidr"]):
            if cidr not in cidrs:
                cidrs.append(cidr)
    networks = []
    for team in current:
        for cidr in team["traffic_cidrs"]:
            network = ipaddress.ip_network(cidr)
            if network.prefixlen == 0 or any(
                owner != team["ip"]
                and other.version == network.version
                and other.overlaps(network)
                for owner, other in networks
            ):
                raise ValueError(
                    "traffic_cidrs overlap between teams or contain a default route"
                )
            networks.append((team["ip"], network))
    return document


def check_wireguard(data, expected):
    sections = []
    for line in data.decode().splitlines():
        line = line.split("#", 1)[0].strip()
        if not line:
            continue
        if line in ("[Interface]", "[Peer]"):
            sections.append((line, {}))
        elif sections and "=" in line:
            key, value = (part.strip() for part in line.split("=", 1))
            if key in sections[-1][1]:
                raise ValueError("Duplicate WireGuard setting")
            sections[-1][1][key] = value
        else:
            raise ValueError("Invalid WireGuard config")
    interfaces = [value for name, value in sections if name == "[Interface]"]
    peers = [value for name, value in sections if name == "[Peer]"]
    if len(interfaces) != 1:
        raise ValueError("Expected one WireGuard Interface section")
    interface = interfaces[0]
    if set(interface) - {"PrivateKey", "Address", "ListenPort", "MTU"}:
        raise ValueError(
            "Remove legacy WireGuard hooks and routing overrides before installation"
        )
    if interface.get("Address") != expected["address"] or interface.get(
        "ListenPort"
    ) != str(expected["port"]):
        raise ValueError("WireGuard server address/port does not match this bundle")
    allowed = [peer.get("AllowedIPs") for peer in peers]
    if len(set(allowed)) != len(allowed) or set(allowed) != set(expected["peers"]):
        raise ValueError("WireGuard peer AllowedIPs do not match this bundle")
    keys = [interface.get("PrivateKey", "")]
    public_keys = [peer.get("PublicKey", "") for peer in peers]
    if len(set(public_keys)) != len(public_keys):
        raise ValueError("Duplicate WireGuard peer public key")
    keys += public_keys + [
        peer["PresharedKey"] for peer in peers if "PresharedKey" in peer
    ]
    for key in keys:
        try:
            valid = len(base64.b64decode(key, validate=True)) == 32
        except ValueError:
            valid = False
        if not valid:
            raise ValueError("Invalid WireGuard key encoding")

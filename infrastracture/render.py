#!/usr/bin/env python3
"""Render reviewed host configs without applying them or generating VPN keys."""

import argparse
import ipaddress
import json
import os
import re
import shlex
import shutil
import tarfile
import tempfile
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parent
NON_PUBLIC = (
    "0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, "
    "172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24, 192.168.0.0/16, "
    "198.18.0.0/15, 198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 240.0.0.0/4"
)


def exact_keys(value, names, location):
    if not isinstance(value, dict) or set(value) != set(names):
        raise ValueError(f"{location}: expected keys {', '.join(sorted(names))}")


def network_layout(config):
    settings = config["network"]
    parsed = {
        name: ipaddress.IPv4Network(settings[name], strict=True)
        for name in ("router_vpn", "forcad", "participant_vpn", "admin_vpn")
    }
    team_base = ipaddress.IPv4Network(settings["team_network_base"], strict=True)
    if not 29 <= parsed["router_vpn"].prefixlen <= 30:
        raise ValueError("network.router_vpn must leave two usable host addresses")
    if not 24 <= parsed["forcad"].prefixlen <= 30:
        raise ValueError("network.forcad must be between /24 and /30")
    if not 24 <= parsed["admin_vpn"].prefixlen <= 30:
        raise ValueError("network.admin_vpn must be between /24 and /30")
    if not 24 <= parsed["participant_vpn"].prefixlen <= 29:
        raise ValueError("network.participant_vpn must fit one /29 per team")
    if team_base.prefixlen != 24:
        raise ValueError("network.team_network_base must be a /24 network")

    team_networks = []
    base_address = int(team_base.network_address)
    for number in range(len(config["teams"]["entries"])):
        address = base_address + number * 256
        if address > 0xFFFFFFFF:
            raise ValueError("Team network allocation exceeds the IPv4 address space")
        team_networks.append(ipaddress.IPv4Network((address, 24)))

    all_networks = [*parsed.values(), *team_networks]
    for index, network in enumerate(all_networks):
        if any(network.overlaps(other) for other in all_networks[index + 1 :]):
            raise ValueError("Configured internal networks must not overlap")

    def first_host(network):
        return ipaddress.IPv4Address(int(network.network_address) + 1)

    def second_host(network):
        return ipaddress.IPv4Address(int(network.network_address) + 2)

    public_host = ipaddress.IPv4Address(config["env"]["public_host"])
    if public_host not in parsed["forcad"] or public_host in {
        parsed["forcad"].network_address,
        parsed["forcad"].broadcast_address,
        first_host(parsed["forcad"]),
    }:
        raise ValueError("env.public_host must be a usable host in network.forcad")
    if parsed["participant_vpn"].num_addresses // 8 < len(team_networks):
        raise ValueError("network.participant_vpn must provide one /29 per team")
    participant_groups = list(parsed["participant_vpn"].subnets(new_prefix=29))
    participant_server = first_host(parsed["participant_vpn"])
    for group in participant_groups[: len(team_networks)]:
        available = sum(address != participant_server for address in group.hosts())
        if available < config["teams"]["members_per_team"]:
            raise ValueError("network.participant_vpn has too few addresses for a team")
    admin_server = first_host(parsed["admin_vpn"])
    if parsed["admin_vpn"].num_addresses - 3 < config["admins"]:
        raise ValueError(
            "network.admin_vpn has too few addresses for configured admins"
        )
    vlan_start = settings["team_vlan_start"]
    if vlan_start + len(team_networks) - 1 > 4094:
        raise ValueError("network.team_vlan_start allocates a VLAN above 4094")
    return {
        **parsed,
        "team_networks": team_networks,
        "router_vpn_router": first_host(parsed["router_vpn"]),
        "router_vpn_vpn": second_host(parsed["router_vpn"]),
        "forcad_router": first_host(parsed["forcad"]),
        "forcad_host": public_host,
        "participant_server": participant_server,
        "admin_server": admin_server,
        "team_vlan_start": vlan_start,
        "allowed_networks": [
            parsed["router_vpn"],
            parsed["forcad"],
            parsed["participant_vpn"],
            parsed["admin_vpn"],
            *team_networks,
        ],
    }


def integer(value, low, high, name):
    if type(value) is not int or not low <= value <= high:
        raise ValueError(f"{name}: expected an integer from {low} to {high}")


def interface(value):
    if (
        not isinstance(value, str)
        or not re.fullmatch(r"[A-Za-z][A-Za-z0-9_-]{0,14}", value)
        or value == "lo"
    ):
        raise ValueError("Interface names must be simple Linux device names, not lo")


def safe_asset(value, directory, location):
    if (
        not isinstance(value, str)
        or not value
        or value.startswith("/")
        or "\\" in value
        or any(part in {"", ".", ".."} for part in value.split("/"))
    ):
        raise ValueError(f"{location} must be a safe relative path")
    base = ROOT.parent / directory
    target = base / value
    resolved_base = base.resolve()
    resolved_target = target.resolve()
    if (
        not resolved_target.is_relative_to(resolved_base)
        or not resolved_target.is_file()
    ):
        raise ValueError(f"{location} does not name a regular file under {directory}")
    cursor = target
    while cursor != base.parent:
        if cursor.is_symlink():
            raise ValueError(f"{location} cannot use symlinks")
        cursor = cursor.parent
    return target


def load_config(path):
    with Path(path).open("rb") as stream:
        config = tomllib.load(stream)
    required = {
        "teams",
        "admins",
        "game_tcp_ports",
        "game_udp_ports",
        "management_tcp_ports",
        "dns_servers",
        "env",
        "game",
        "tasks",
        "router",
        "vpn",
        "forcad",
        "network",
    }
    if set(config) != required:
        raise ValueError("settings: unexpected or missing top-level keys")
    exact_keys(
        config["network"],
        (
            "router_vpn",
            "forcad",
            "participant_vpn",
            "admin_vpn",
            "team_network_base",
            "team_vlan_start",
        ),
        "network",
    )
    exact_keys(
        config["router"],
        (
            "wan_interface",
            "wan_address",
            "wan_gateway",
            "vpn_interface",
            "forcad_interface",
            "team_trunk",
        ),
        "router",
    )
    exact_keys(
        config["vpn"],
        ("interface", "endpoint", "participant_port", "admin_port"),
        "vpn",
    )
    exact_keys(config["teams"], ("members_per_team", "entries"), "teams")
    exact_keys(
        config["env"],
        (
            "public_host",
            "public_scheme",
            "brand_name",
            "brand_logo",
            "brand_primary",
            "brand_accent",
            "monitoring",
            "router_metrics",
            "game_metrics",
        ),
        "env",
    )
    exact_keys(
        config["game"],
        (
            "mode",
            "round_time",
            "rounds",
            "start_time",
            "timezone",
            "default_score",
            "flag_lifetime",
            "game_hardness",
            "inflation",
            "volga_attacks_mode",
            "checkers_path",
            "env_path",
            "get_period",
        ),
        "game",
    )
    exact_keys(config["tasks"], ("entries",), "tasks")
    exact_keys(
        config["forcad"],
        ("interface", "repository_url", "revision", "install_path"),
        "forcad",
    )
    team_config = config["teams"]
    integer(team_config["members_per_team"], 1, 5, "teams.members_per_team")
    entries = team_config["entries"]
    if not isinstance(entries, list) or not 1 <= len(entries) <= 32:
        raise ValueError("teams.entries must contain 1 to 32 teams")
    names = set()
    for index, team in enumerate(entries, 1):
        exact_keys(team, ("name", "logo", "highlighted"), f"teams.entries[{index}]")
        if (
            not isinstance(team["name"], str)
            or not team["name"].strip()
            or len(team["name"]) > 128
        ):
            raise ValueError(f"teams.entries[{index}].name must be 1 to 128 characters")
        if team["name"] in names:
            raise ValueError(f"Duplicate team name: {team['name']}")
        names.add(team["name"])
        if type(team["highlighted"]) is not bool:
            raise ValueError(f"teams.entries[{index}].highlighted must be a boolean")
        if team["logo"] == "":
            team["logo"] = None
        elif team["logo"] is not None:
            safe_asset(team["logo"], "public/teams", f"teams.entries[{index}].logo")
    env = config["env"]
    for field in (
        "public_host",
        "brand_name",
        "brand_logo",
        "brand_primary",
        "brand_accent",
    ):
        if not isinstance(env[field], str) or not env[field].strip():
            raise ValueError(f"env.{field} must be a nonempty string")
    try:
        ipaddress.IPv4Address(env["public_host"])
    except ipaddress.AddressValueError as error:
        raise ValueError("env.public_host must be an IPv4 address") from error
    network = config["network"]
    for field in (
        "router_vpn",
        "forcad",
        "participant_vpn",
        "admin_vpn",
        "team_network_base",
    ):
        if not isinstance(network[field], str):
            raise TypeError(f"network.{field} must be an IPv4 network")
    integer(network["team_vlan_start"], 1, 4094, "network.team_vlan_start")
    if any(char in env["brand_name"] for char in '\r\n\x00"\\'):
        raise ValueError("env.brand_name contains unsupported characters")
    if env["public_scheme"] not in {"http", "https"}:
        raise ValueError("env.public_scheme must be http or https")
    for field in ("monitoring", "router_metrics", "game_metrics"):
        if type(env[field]) is not bool:
            raise ValueError(f"env.{field} must be a boolean")
    if not re.fullmatch(r"#[0-9A-Fa-f]{6}", env["brand_primary"]):
        raise ValueError("env.brand_primary must use #RRGGBB")
    if not re.fullmatch(r"#[0-9A-Fa-f]{6}", env["brand_accent"]):
        raise ValueError("env.brand_accent must use #RRGGBB")
    safe_asset(env["brand_logo"], "public/brand", "env.brand_logo")
    game = config["game"]
    if game["mode"] not in {"classic", "blitz"}:
        raise ValueError("game.mode must be classic or blitz")
    for field in ("round_time", "default_score", "flag_lifetime"):
        integer(game[field], 1, 2_147_483_647, f"game.{field}")
    if game["rounds"] is not None:
        integer(game["rounds"], 1, 2_147_483_647, "game.rounds")
    if game["get_period"] is not None:
        integer(game["get_period"], 1, 2_147_483_647, "game.get_period")
    if not isinstance(game["start_time"], str) or not game["start_time"].strip():
        raise ValueError("game.start_time must be a nonempty string")
    if not isinstance(game["timezone"], str) or not game["timezone"].strip():
        raise ValueError("game.timezone must be a nonempty string")
    if type(game["game_hardness"]) not in (float, int) or game["game_hardness"] <= 1:
        raise ValueError("game.game_hardness must be greater than 1")
    for field in ("inflation", "volga_attacks_mode"):
        if type(game[field]) is not bool:
            raise ValueError(f"game.{field} must be a boolean")
    for field in ("checkers_path", "env_path"):
        if not isinstance(game[field], str) or not game[field].startswith("/"):
            raise ValueError(f"game.{field} must be an absolute path")
    task_entries = config["tasks"]["entries"]
    if not isinstance(task_entries, list) or not task_entries:
        raise ValueError("tasks.entries must contain at least one task")
    task_names = set()
    for index, task in enumerate(task_entries, 1):
        exact_keys(
            task,
            (
                "name",
                "checker",
                "checker_timeout",
                "checker_type",
                "puts",
                "gets",
                "places",
                "default_score",
                "env_path",
                "get_period",
            ),
            f"tasks.entries[{index}]",
        )
        if (
            not isinstance(task["name"], str)
            or not task["name"].strip()
            or task["name"] in task_names
        ):
            raise ValueError(f"tasks.entries[{index}].name must be unique and nonempty")
        task_names.add(task["name"])
        checker = task["checker"]
        safe_asset(checker, "checkers", f"tasks.entries[{index}].checker")
        if (
            not isinstance(task["checker_type"], str)
            or not task["checker_type"].strip()
        ):
            raise ValueError(f"tasks.entries[{index}].checker_type must be nonempty")
        for field, low, high in (
            ("checker_timeout", 1, 2_147_483_647),
            ("puts", 0, 1024),
            ("gets", 0, 1024),
            ("places", 1, 2_147_483_647),
            ("default_score", 1, 2_147_483_647),
        ):
            integer(task[field], low, high, f"tasks.entries[{index}].{field}")
        if task["puts"] + task["gets"] > 1024:
            raise ValueError(
                f"tasks.entries[{index}]: puts + gets must not exceed 1024"
            )
        if task["env_path"] is not None and (
            not isinstance(task["env_path"], str)
            or not task["env_path"].startswith("/")
        ):
            raise ValueError(
                f"tasks.entries[{index}].env_path must be an absolute path"
            )
        if task["get_period"] is not None:
            integer(
                task["get_period"],
                1,
                2_147_483_647,
                f"tasks.entries[{index}].get_period",
            )
    for field in ("repository_url", "revision", "install_path"):
        if (
            not isinstance(config["forcad"][field], str)
            or not config["forcad"][field].strip()
        ):
            raise ValueError(f"forcad.{field} must be a nonempty string")
    install_path = config["forcad"]["install_path"]
    if (
        not install_path.startswith("/")
        or install_path == "/"
        or any(part in {"", ".", ".."} for part in install_path.split("/")[1:])
        or any(char in install_path for char in "\r\n\x00")
    ):
        raise ValueError("forcad.install_path must be an absolute non-root path")
    if not re.fullmatch(r"https://[^\s]+\.git", config["forcad"]["repository_url"]):
        raise ValueError(
            "forcad.repository_url must be an HTTPS Git URL ending in .git"
        )
    if not re.fullmatch(r"[A-Za-z0-9._/-]+", config["forcad"]["revision"]):
        raise ValueError("forcad.revision contains unsupported characters")
    integer(config["admins"], 1, 253, "admins")
    for name in ("game_tcp_ports", "game_udp_ports", "management_tcp_ports"):
        ports = config[name]
        if not isinstance(ports, list):
            raise TypeError(f"{name}: expected a port list")
        for port in ports:
            integer(port, 1, 65535, name)
        if len(ports) != len(set(ports)):
            raise ValueError(f"{name}: duplicate port")
    if not config["management_tcp_ports"]:
        raise ValueError("management_tcp_ports must not be empty")
    if set(config["game_tcp_ports"]) & set(config["management_tcp_ports"]):
        raise ValueError("Game TCP ports must not overlap team management ports")
    for name in ("participant_port", "admin_port"):
        integer(config["vpn"][name], 1, 65535, f"vpn.{name}")
    if config["vpn"]["participant_port"] == config["vpn"]["admin_port"]:
        raise ValueError("Participant and admin VPN ports must differ")
    router = config["router"]
    router_interfaces = [
        router[key]
        for key in ("wan_interface", "vpn_interface", "forcad_interface", "team_trunk")
    ]
    for name in [
        *router_interfaces,
        config["vpn"]["interface"],
        config["forcad"]["interface"],
    ]:
        interface(name)
    if len(set(router_interfaces)) != 4:
        raise ValueError("Router interfaces must be distinct")
    if len(router["team_trunk"]) + 4 > 15:
        raise ValueError("Team trunk name is too long for VLAN interface names")
    if config["vpn"]["interface"] in {"wg-attack", "wg-admin"}:
        raise ValueError("VPN transit interface must differ from WireGuard interfaces")
    wan = ipaddress.IPv4Interface(router["wan_address"])
    gateway = ipaddress.IPv4Address(router["wan_gateway"])
    if gateway not in wan.network or gateway == wan.ip:
        raise ValueError("WAN gateway must be a different address in the WAN subnet")
    if wan.network.prefixlen < 31 and (
        {gateway, wan.ip} & {wan.network.network_address, wan.network.broadcast_address}
    ):
        raise ValueError("WAN address and gateway must be host addresses")
    layout = network_layout(config)
    if (
        any(wan.network.overlaps(internal) for internal in layout["allowed_networks"])
        or wan.network.overlaps(layout["router_vpn"])
        or wan.ip.is_loopback
        or wan.ip.is_multicast
        or wan.ip.is_unspecified
    ):
        raise ValueError(
            "WAN must not overlap an internal network or use a special host address"
        )
    endpoint = config["vpn"]["endpoint"]
    if not isinstance(endpoint, str) or len(endpoint) > 253:
        raise ValueError(
            "VPN endpoint must be an IPv4 address or DNS name without a port"
        )
    if not all(
        re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", label)
        for label in endpoint.split(".")
    ):
        raise ValueError(
            "VPN endpoint must be an IPv4 address or DNS name without a port"
        )
    if not isinstance(config["dns_servers"], list) or not config["dns_servers"]:
        raise ValueError("dns_servers must be a nonempty IPv4 list")
    for address in config["dns_servers"]:
        parsed = ipaddress.IPv4Address(address)
        if not parsed.is_global:
            raise ValueError(
                "DNS servers must be public IPv4 addresses reachable through WAN"
            )
    return config


def teams(config):
    result = []
    layout = network_layout(config)
    subnets = list(layout["participant_vpn"].subnets(new_prefix=29))
    team_config = config["teams"]
    for number, entry in enumerate(team_config["entries"], 1):
        vlan = layout["team_vlan_start"] + number - 1
        group = subnets[number - 1]
        peers = [str(ip) for ip in group.hosts() if ip != layout["participant_server"]][
            : team_config["members_per_team"]
        ]
        team_network = layout["team_networks"][number - 1]
        result.append(
            {
                "id": number,
                "name": entry["name"],
                "logo": entry["logo"],
                "highlighted": entry["highlighted"],
                "vlan": vlan,
                "interface": f"{config['router']['team_trunk']}.{vlan}",
                "network": str(team_network),
                "gateway": str(team_network.network_address + 1),
                "vulnbox": str(team_network.network_address + 2),
                "vpn_cidr": str(group),
                "peers": peers,
            }
        )
    return result


def nft_set(values, quote=False):
    return (
        "{ "
        + ", ".join(json.dumps(value) if quote else str(value) for value in values)
        + " }"
    )


def template(relative, **values):
    content = (ROOT / relative).read_text()
    for name, value in values.items():
        content = content.replace(f"@@{name}@@", value)
    if re.search(r"@@[A-Z_]+@@", content):
        raise ValueError(f"Unresolved placeholder in {relative}")
    return content


def ownership_rules(config, action):
    ports = nft_set(config["management_tcp_ports"])
    return "\n".join(
        f"        ct original ip saddr {team['vpn_cidr']} ct original ip daddr {team['vulnbox']} "
        f"meta l4proto tcp ct original proto-dst {ports} {action}"
        for team in teams(config)
    )


def router_rules(config):
    router = config["router"]
    team_list = teams(config)
    layout = network_layout(config)
    definitions = {
        "wan_if": json.dumps(router["wan_interface"]),
        "wan_ip": str(ipaddress.IPv4Interface(router["wan_address"]).ip),
        "vpn_if": json.dumps(router["vpn_interface"]),
        "vpn_ip": str(layout["router_vpn_vpn"]),
        "forcad_if": json.dumps(router["forcad_interface"]),
        "forcad_ip": str(layout["forcad_host"]),
        "attack_net": str(layout["participant_vpn"]),
        "admin_net": str(layout["admin_vpn"]),
        "wg_ports": nft_set(
            [
                config["vpn"]["participant_port"],
                config["vpn"]["admin_port"],
            ]
        ),
        "team_ifs": nft_set([team["interface"] for team in team_list], quote=True),
        "team_nets": nft_set([team["network"] for team in team_list]),
        "vulnboxes": nft_set([team["vulnbox"] for team in team_list]),
        "non_public_nets": "{ " + NON_PUBLIC + " }",
    }
    game_rules = []
    for protocol in ("tcp", "udp"):
        ports = config[f"game_{protocol}_ports"]
        if ports:
            game_rules.append(
                f"        meta l4proto {protocol} ct original proto-dst {nft_set(ports)} jump allowed_flow"
            )
    return template(
        "router/common.nft.in",
        DEFINITIONS="\n".join(
            f"define {name} = {value}" for name, value in definitions.items()
        ),
        SOURCE_CHECKS="\n".join(
            f'        iifname "{team["interface"]}" ip saddr != {team["vulnbox"]} counter drop'
            for team in team_list
        ),
        GAME_PORTS="\n".join(game_rules),
        OWN_MANAGEMENT=ownership_rules(config, "jump allowed_flow"),
        SNAT="\n".join(
            f'        oifname "{team["interface"]}" ip daddr {team["vulnbox"]} '
            f"ip saddr {{ $forcad_ip, $attack_net, $admin_net }} snat to {team['gateway']}"
            for team in team_list
        ),
    )


def vpn_rules(config):
    layout = network_layout(config)
    return template(
        "vpn/firewall.nft.in",
        DEFINITIONS=f'''define transit_if = "{config["vpn"]["interface"]}"
define router_ip = {layout["router_vpn_router"]}
define attack_net = {layout["participant_vpn"]}
define admin_net = {layout["admin_vpn"]}
define wg_ports = {nft_set([config["vpn"]["participant_port"], config["vpn"]["admin_port"]])}''',
        GROUPS="\n".join(
            f"        ip saddr {team['vpn_cidr']} ip daddr {team['vpn_cidr']} counter accept"
            for team in teams(config)
        ),
    )


def interfaces(config, role):
    dns = " ".join(config["dns_servers"])
    layout = network_layout(config)
    header = "# Merge with the host's existing /etc/network/interfaces.\n# Do not configure the same device in two network managers.\n\nauto lo\niface lo inet loopback\n"
    if role != "router":
        name = config[role]["interface"]
        if role == "vpn":
            address = f"{layout['router_vpn_vpn']}/{layout['router_vpn'].prefixlen}"
            gateway = str(layout["router_vpn_router"])
        else:
            address = f"{layout['forcad_host']}/{layout['forcad'].prefixlen}"
            gateway = str(layout["forcad_router"])
        return (
            header
            + f"\nauto {name}\niface {name} inet static\n    address {address}\n    gateway {gateway}\n    dns-nameservers {dns}\n"
        )
    router = config["router"]
    participant_net = layout["participant_vpn"]
    admin_net = layout["admin_vpn"]
    result = (
        header
        + f"""
auto {router["wan_interface"]}
iface {router["wan_interface"]} inet static
    address {router["wan_address"]}
    gateway {router["wan_gateway"]}
    dns-nameservers {dns}

auto {router["vpn_interface"]}
iface {router["vpn_interface"]} inet static
    address {layout["router_vpn_router"]}/{layout["router_vpn"].prefixlen}
    up ip route replace {participant_net} via {layout["router_vpn_vpn"]} dev {router["vpn_interface"]}
    up ip route replace {admin_net} via {layout["router_vpn_vpn"]} dev {router["vpn_interface"]}
    down ip route del {participant_net} via {layout["router_vpn_vpn"]} dev {router["vpn_interface"]} || true
    down ip route del {admin_net} via {layout["router_vpn_vpn"]} dev {router["vpn_interface"]} || true

auto {router["forcad_interface"]}
iface {router["forcad_interface"]} inet static
    address {layout["forcad_router"]}/{layout["forcad"].prefixlen}

auto {router["team_trunk"]}
iface {router["team_trunk"]} inet manual
"""
    )
    for team in teams(config):
        result += f"\nauto {team['interface']}\niface {team['interface']} inet static\n    address {team['gateway']}/{ipaddress.IPv4Network(team['network']).prefixlen}\n    vlan-raw-device {router['team_trunk']}\n"
    return result


def firewall_service(role):
    path = "active.nft" if role == "router" else "firewall.nft"
    return f"""[Unit]
Description=ForcAD {role} firewall
DefaultDependencies=no
After=local-fs.target
Before=network-pre.target shutdown.target
Wants=network-pre.target
Conflicts=shutdown.target

[Service]
Type=oneshot
ExecStart=/usr/sbin/nft -f /etc/forcad/{role}/{path}
ExecReload=/usr/sbin/nft -f /etc/forcad/{role}/{path}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
"""


def game_metrics(config):
    inventory = teams(config)
    layout = network_layout(config)
    peer_map = ",\n".join(
        f"    [{peer}] = {team['vulnbox']}"
        for team in inventory
        for peer in team["peers"]
    )
    targets = ", ".join(team["vulnbox"] for team in inventory)
    ports = ", ".join(
        f"{port}/{protocol}"
        for protocol in ("tcp", "udp")
        for port in config[f"game_{protocol}_ports"]
    )
    # Keep IP fragments too: a port-only BPF would lose non-initial fragments.
    target_filter = " or ".join(f"host {team['vulnbox']}" for team in inventory)
    capture_filter = f"ip and net {layout['participant_vpn']} and ({target_filter})"
    return {
        "router/game-metrics.zeek": (ROOT / "router/game-metrics.zeek").read_text(),
        "router/game-inventory.zeek": (
            "# Public mapping; keep synchronized with WireGuard and ForcAD.\n"
            f"redef Forcad::peers = {{\n{peer_map}\n}};\n"
            f"redef Forcad::vulnboxes = {{ {targets} }};\n"
            f"redef Forcad::game_ports = {{ {ports} }};\n"
            f'redef Telemetry::metrics_address = "{layout["forcad_router"]}";\n'
            "redef Telemetry::metrics_port = 9911/tcp;\n"
            'redef Telemetry::metrics_endpoint_name = "router";\n'
        ),
        "router/game-metrics.service": template(
            "router/game-metrics.service.in",
            INTERFACE=config["router"]["vpn_interface"],
            FILTER=capture_filter,
        ),
        "forcad/game-sensors.yml": (f'- targets: ["{layout["forcad_router"]}:9911"]\n'),
    }


def yaml_value(value):
    return json.dumps(value, ensure_ascii=False)


def forcad_config(config):
    game = config["game"]
    lines = ["game:"]
    for key, value in game.items():
        if value is not None:
            lines.append(f"  {key}: {yaml_value(value)}")
    lines.append("tasks:")
    for task in config["tasks"]["entries"]:
        lines.append("  - name: " + yaml_value(task["name"]))
        for key, value in task.items():
            if key == "name" or value is None:
                continue
            lines.append(f"    {key}: {yaml_value(value)}")
    lines.append("teams:")
    for team in teams(config):
        lines.append("  - name: " + yaml_value(team["name"]))
        lines.append(f"    ip: {team['vulnbox']}")
        lines.append(f"    highlighted: {str(team['highlighted']).lower()}")
        if team["logo"]:
            lines.append("    logo_path: " + yaml_value("/team-logos/" + team["logo"]))
        lines.append("    token: null")
        lines.append("    traffic_cidrs:")
        lines.append(f"      - {team['vulnbox']}/32")
        lines.append(f"      - {team['vpn_cidr']}")
    return "\n".join(lines) + "\n"


def forcad_environment(config):
    env = config["env"]
    values = {
        "FORCAD_NGINX_BIND": env["public_host"],
        "PUBLIC_ORIGIN": f"{env['public_scheme']}://{env['public_host']}",
        "PUBLIC_SCHEME": env["public_scheme"],
        "VITE_BRAND_NAME": env["brand_name"],
        "VITE_BRAND_LOGO": "/brand/" + env["brand_logo"],
        "VITE_BRAND_PRIMARY": env["brand_primary"],
        "VITE_BRAND_ACCENT": env["brand_accent"],
    }
    return "".join(f'{key}="{value}"\n' for key, value in values.items())


def forcad_assets(config):
    files = {}
    brand_logo = config["env"]["brand_logo"]
    files[f"forcad/public/brand/{brand_logo}"] = safe_asset(
        brand_logo, "public/brand", "env.brand_logo"
    ).read_bytes()
    for team in teams(config):
        if team["logo"]:
            logo = team["logo"]
            files[f"forcad/public/teams/{logo}"] = safe_asset(
                logo, "public/teams", f"team {team['name']} logo"
            ).read_bytes()
    copied = set()
    for task in config["tasks"]["entries"]:
        checker = safe_asset(
            task["checker"], "checkers", f"task {task['name']} checker"
        )
        folder = checker.parent
        for path in sorted(folder.rglob("*")):
            if not path.is_file() or path.is_symlink():
                continue
            relative = path.relative_to(ROOT.parent / "checkers")
            if relative in copied:
                continue
            copied.add(relative)
            files[f"forcad/checkers/{relative.as_posix()}"] = path.read_bytes()
    requirements = ROOT.parent / "checkers/requirements.txt"
    if requirements.is_file():
        files["forcad/checkers/requirements.txt"] = requirements.read_bytes()
    return files


def host_bundle(config):
    result = {}
    router = config["router"]
    inventory = teams(config)
    layout = network_layout(config)
    for role in ("router", "vpn", "forcad"):
        bundle_root = f"{role}/"
        host = {"role": role, "teams": inventory}
        if role == "router":
            host["devices"] = [
                router[key]
                for key in (
                    "wan_interface",
                    "vpn_interface",
                    "forcad_interface",
                    "team_trunk",
                )
            ]
            host["addresses"] = [
                [router["wan_interface"], router["wan_address"]],
                [
                    router["vpn_interface"],
                    f"{layout['router_vpn_router']}/{layout['router_vpn'].prefixlen}",
                ],
                [
                    router["forcad_interface"],
                    f"{layout['forcad_router']}/{layout['forcad'].prefixlen}",
                ],
                *[
                    [
                        team["interface"],
                        f"{team['gateway']}/{ipaddress.IPv4Network(team['network']).prefixlen}",
                    ]
                    for team in inventory
                ],
            ]
            host["routes"] = [
                {
                    "dst": "default",
                    "gateway": router["wan_gateway"],
                    "dev": router["wan_interface"],
                },
                *[
                    {
                        "dst": str(net),
                        "gateway": str(layout["router_vpn_vpn"]),
                        "dev": router["vpn_interface"],
                    }
                    for net in (layout["participant_vpn"], layout["admin_vpn"])
                ],
            ]
            host["ping"] = [
                str(layout["router_vpn_vpn"]),
                str(layout["forcad_host"]),
                *[team["vulnbox"] for team in inventory],
            ]
        else:
            name = config[role]["interface"]
            if role == "vpn":
                address = f"{layout['router_vpn_vpn']}/{layout['router_vpn'].prefixlen}"
                gateway = str(layout["router_vpn_router"])
            else:
                address = f"{layout['forcad_host']}/{layout['forcad'].prefixlen}"
                gateway = str(layout["forcad_router"])
            host["devices"] = [name]
            host["addresses"] = [[name, address]]
            host["routes"] = [{"dst": "default", "gateway": gateway, "dev": name}]
            host["ping"] = [gateway]
            if role == "vpn":
                host["wireguard"] = {
                    "wg-attack": {
                        "address": (
                            f"{layout['participant_server']}/"
                            f"{layout['participant_vpn'].prefixlen}"
                        ),
                        "port": config["vpn"]["participant_port"],
                        "peers": [
                            peer + "/32" for team in inventory for peer in team["peers"]
                        ],
                    },
                    "wg-admin": {
                        "address": (
                            f"{layout['admin_server']}/{layout['admin_vpn'].prefixlen}"
                        ),
                        "port": config["vpn"]["admin_port"],
                        "peers": [
                            f"{layout['admin_vpn'].network_address + number + 2}/32"
                            for number in range(config["admins"])
                        ],
                    },
                }
                host["addresses"] += [
                    [name, value["address"]]
                    for name, value in host["wireguard"].items()
                ]
        host["checks"] = {
            "forcad_url": f"http://{layout['forcad_host']}/api/client/status/",
            "metrics_address": str(layout["forcad_router"]),
        }
        result[f"{bundle_root}host.json"] = json.dumps(host, indent=2) + "\n"
        result[f"{bundle_root}host.py"] = (ROOT / role / "host.py").read_text()
        for module in sorted((ROOT / "hostlib").glob("*.py")):
            result[f"{bundle_root}hostlib/{module.name}"] = module.read_text()
        if role == "router":
            for module in sorted((ROOT / "router/routerlib").glob("*.py")):
                result[f"{bundle_root}routerlib/{module.name}"] = module.read_text()
        if role == "vpn":
            for module in sorted((ROOT / "vpn/vpnlib").glob("*.py")):
                result[f"{bundle_root}vpnlib/{module.name}"] = module.read_text()
        result[f"{bundle_root}APPLY.txt"] = (ROOT / role / "APPLY.txt").read_text()
        result[f"{bundle_root}UNINSTALL.txt"] = (
            ROOT / role / "UNINSTALL.txt"
        ).read_text()
        for action in ("install", "test", "uninstall"):
            script_root = f"{role}/host/" if role == "forcad" else bundle_root
            host_script = (
                f'"$bundle_dir/../host.py" {action} "$@"'
                if role == "forcad"
                else f'"$bundle_dir/host.py" {action} "$@"'
            )
            sudo_script = (
                f'exec sudo -- python3 "$bundle_dir/../host.py" {action} "$@"'
                if role == "forcad"
                else f'exec sudo -- python3 "$bundle_dir/host.py" {action} "$@"'
            )
            result[f"{script_root}{action}.sh"] = f"""#!/usr/bin/env bash
set -euo pipefail
bundle_dir="$(cd -- "$(dirname -- "${{BASH_SOURCE[0]}}")" && pwd)"
if [[ "$EUID" -ne 0 && "${{1:-}}" != "--help" ]]; then
    {sudo_script.removeprefix("exec ")}
fi
exec python3 {host_script}
"""
    return result


def fresh_forcad_bundle(config):
    settings = config["forcad"]
    replacements = {
        "INSTALL_PATH": shlex.quote(settings["install_path"]),
        "REPOSITORY_URL": shlex.quote(settings["repository_url"]),
        "REVISION": shlex.quote(settings["revision"]),
        "INSTALL_MARKER": shlex.quote(
            settings["repository_url"] + " " + settings["revision"]
        ),
        "MONITORING": "true" if config["env"]["monitoring"] else "false",
        "METRICS_OPTIONS": " ".join(
            option
            for enabled, option in (
                (config["env"]["router_metrics"], "--network-metrics"),
                (config["env"]["game_metrics"], "--game-metrics"),
            )
            if enabled
        ),
    }
    return {
        f"forcad/{name}": template(f"forcad/{name}.in", **replacements)
        for name in ("install.sh", "test.sh", "uninstall.sh")
    }


def fresh_forcad_files(config):
    files = {
        "forcad/config.yml": forcad_config(config),
        "forcad/environment.env": forcad_environment(config),
        "forcad/host/environment.env": (
            'FORCAD_NGINX_BIND="'
            + config["env"]["public_host"]
            + '"\n'
            + 'PUBLIC_ORIGIN="'
            + config["env"]["public_scheme"]
            + "://"
            + config["env"]["public_host"]
            + '"\nPUBLIC_SCHEME="'
            + config["env"]["public_scheme"]
            + '"\n'
        ),
    }
    files.update(fresh_forcad_bundle(config))
    files.update(forcad_assets(config))
    files.update(
        {
            "forcad/host/APPLY.txt": (ROOT / "forcad/APPLY.txt").read_text(),
            "forcad/host/UNINSTALL.txt": (ROOT / "forcad/UNINSTALL.txt").read_text(),
        }
    )
    return files


def render(config):
    files = {}
    layout = network_layout(config)
    for role in ("router", "vpn", "forcad"):
        files[f"{role}/interfaces"] = interfaces(config, role)
    sysctl = """net.ipv4.ip_forward=1
net.ipv4.conf.all.send_redirects=0
net.ipv4.conf.default.send_redirects=0
net.ipv4.conf.all.accept_redirects=0
net.ipv4.conf.default.accept_redirects=0
net.ipv4.conf.all.accept_source_route=0
net.ipv4.conf.default.accept_source_route=0
net.ipv4.conf.all.rp_filter=2
net.ipv4.conf.default.rp_filter=2
net.ipv6.conf.all.disable_ipv6=1
net.ipv6.conf.default.disable_ipv6=1
"""
    for role in ("router", "vpn"):
        files[f"{role}/sysctl.conf"] = sysctl
        files[f"{role}/forcad-{role}-firewall.service"] = firewall_service(role)
        files[f"{role}/networking.conf"] = (
            "[Unit]\n"
            f"Requires=forcad-{role}-firewall.service\n"
            f"After=forcad-{role}-firewall.service\n"
        )
    files["router/common.nft"] = router_rules(config)
    include = 'include "/etc/forcad/router/common.nft"\n'
    files["router/game.nft"] = "#!/usr/sbin/nft -f\n" + include
    files["router/pre-game.nft"] = (
        "#!/usr/sbin/nft -f\n"
        + include
        + "\n"
        + (
            "add rule inet forcad_router phase_rules ct original ip saddr $attack_net "
            "ct original ip daddr $vulnboxes ct original protocol != icmp counter drop\n"
            "add rule inet forcad_router phase_rules ct original ip saddr $attack_net "
            "ct original ip daddr $vulnboxes icmp type != { echo-request, echo-reply } counter drop\n"
        )
    )
    files["router/active.nft"] = 'include "/etc/forcad/router/pre-game.nft"\n'
    files["router/network-metrics.service"] = (
        (ROOT / "router/network-metrics.service")
        .read_text()
        .replace("@@LISTEN_ADDRESS@@", str(network_layout(config)["forcad_router"]))
    )
    files["router/network-rules.service"] = (
        ROOT / "router/network-rules.service"
    ).read_text()
    files["vpn/firewall.nft"] = vpn_rules(config)
    files["vpn/firewall.conf"] = (ROOT / "vpn/firewall.conf").read_text()
    files["forcad/routers.yml"] = f'- targets: ["{layout["forcad_router"]}:9109"]\n'
    files.update(game_metrics(config))
    files.update(host_bundle(config))
    files.update(fresh_forcad_files(config))
    files["teams.json"] = json.dumps(teams(config), indent=2) + "\n"
    return files


def write_bundle(output, files):
    output = Path(output)
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    previous_umask = os.umask(0o077)
    try:
        for relative, content in files.items():
            path = output / relative
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            if isinstance(content, bytes):
                descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(descriptor, "wb") as stream:
                    stream.write(content)
            else:
                with open(
                    path,
                    "x",
                    encoding="utf-8",
                    opener=lambda name, flags: os.open(name, flags, 0o600),
                ) as stream:
                    stream.write(content)
    except BaseException:
        shutil.rmtree(output)
        raise
    finally:
        os.umask(previous_umask)


def create_archives(output, roles=("vpn", "router", "forcad"), wireguard_server=None):
    output = Path(output)
    for role in roles:
        bundle = output / role
        archive = output / f"{role}.tar.gz"
        if not bundle.is_dir():
            continue
        descriptor, temporary_name = tempfile.mkstemp(
            prefix=f".{role}.", suffix=".tmp", dir=output
        )
        temporary = Path(temporary_name)
        try:
            with (
                os.fdopen(descriptor, "wb") as stream,
                tarfile.open(fileobj=stream, mode="w:gz") as tar,
            ):
                for path in sorted(bundle.rglob("*")):
                    if path.is_symlink():
                        raise ValueError(f"Cannot archive symlink: {path}")
                    if path.is_file():
                        tar.add(path, arcname=path.relative_to(bundle))
                if role == "vpn" and wireguard_server is not None:
                    for path in sorted(Path(wireguard_server).glob("*")):
                        if path.is_symlink() or not path.is_file():
                            raise ValueError(f"Invalid WireGuard server file: {path}")
                        tar.add(path, arcname=Path("server") / path.name)
            os.replace(temporary, archive)
        except BaseException:
            temporary.unlink(missing_ok=True)
            raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=ROOT / "settings.toml")
    parser.add_argument("--output", type=Path, default=ROOT / "runtime")
    args = parser.parse_args()
    output_created = False
    try:
        config = load_config(args.config)
        files = render(config)
        files["settings.toml"] = args.config.read_text()
        write_bundle(args.output, files)
        output_created = True
        create_archives(args.output)
    except (ValueError, TypeError, OSError, tomllib.TOMLDecodeError) as error:
        if output_created and Path(args.output).is_dir():
            shutil.rmtree(args.output)
        parser.exit(1, f"Render failed: {error}\n")
    print(
        f"Rendered {len(config['teams']['entries'])} teams into {args.output}. No host configuration was applied."
    )


if __name__ == "__main__":
    main()

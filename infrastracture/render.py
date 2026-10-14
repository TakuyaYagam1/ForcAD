#!/usr/bin/env python3
"""Render reviewed host configs without applying them or generating VPN keys."""

import argparse
import ipaddress
import json
import os
import re
import shutil
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parent
ATTACK_NET = ipaddress.IPv4Network("10.200.0.0/24")
ADMIN_NET = ipaddress.IPv4Network("10.201.0.0/24")
NON_PUBLIC = (
    "0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, "
    "172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24, 192.168.0.0/16, "
    "198.18.0.0/15, 198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 240.0.0.0/4"
)


def exact_keys(value, names, location):
    if not isinstance(value, dict) or set(value) != set(names):
        raise ValueError(f"{location}: expected keys {', '.join(sorted(names))}")


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


def load_config(path):
    with Path(path).open("rb") as stream:
        config = tomllib.load(stream)
    exact_keys(
        config,
        (
            "teams",
            "members_per_team",
            "admins",
            "game_tcp_ports",
            "game_udp_ports",
            "management_tcp_ports",
            "dns_servers",
            "router",
            "vpn",
            "forcad",
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
    exact_keys(config["forcad"], ("interface",), "forcad")
    integer(config["teams"], 1, 32, "teams")
    integer(config["members_per_team"], 1, 5, "members_per_team")
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
    if (
        wan.network.overlaps(ipaddress.IPv4Network("10.0.0.0/8"))
        or wan.ip.is_loopback
        or wan.ip.is_multicast
        or wan.ip.is_unspecified
    ):
        raise ValueError(
            "WAN must not overlap the internal 10.0.0.0/8 layout or use a special host address"
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
    subnets = list(ATTACK_NET.subnets(new_prefix=29))
    for number in range(1, config["teams"] + 1):
        vlan = 100 + number
        group = subnets[number - 1]
        peers = [str(ip) for ip in group.hosts() if str(ip) != "10.200.0.1"][
            : config["members_per_team"]
        ]
        result.append(
            {
                "id": number,
                "name": f"team-{number:02}",
                "vlan": vlan,
                "interface": f"{config['router']['team_trunk']}.{vlan}",
                "network": f"10.{vlan}.0.0/24",
                "gateway": f"10.{vlan}.0.1",
                "vulnbox": f"10.{vlan}.0.2",
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
    definitions = {
        "wan_if": json.dumps(router["wan_interface"]),
        "wan_ip": str(ipaddress.IPv4Interface(router["wan_address"]).ip),
        "vpn_if": json.dumps(router["vpn_interface"]),
        "vpn_ip": "10.0.1.2",
        "forcad_if": json.dumps(router["forcad_interface"]),
        "forcad_ip": "10.0.2.10",
        "attack_net": str(ATTACK_NET),
        "admin_net": str(ADMIN_NET),
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
    return template(
        "vpn/firewall.nft.in",
        DEFINITIONS=f'''define transit_if = "{config["vpn"]["interface"]}"
define attack_net = {ATTACK_NET}
define admin_net = {ADMIN_NET}
define wg_ports = {nft_set([config["vpn"]["participant_port"], config["vpn"]["admin_port"]])}''',
        GROUPS="\n".join(
            f"        ip saddr {team['vpn_cidr']} ip daddr {team['vpn_cidr']} counter accept"
            for team in teams(config)
        ),
    )


def interfaces(config, role):
    dns = " ".join(config["dns_servers"])
    header = "# Merge with the host's existing /etc/network/interfaces.\n# Do not configure the same device in two network managers.\n\nauto lo\niface lo inet loopback\n"
    if role != "router":
        name = config[role]["interface"]
        address, gateway = (
            ("10.0.1.2/30", "10.0.1.1")
            if role == "vpn"
            else ("10.0.2.10/24", "10.0.2.1")
        )
        return (
            header
            + f"\nauto {name}\niface {name} inet static\n    address {address}\n    gateway {gateway}\n    dns-nameservers {dns}\n"
        )
    router = config["router"]
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
    address 10.0.1.1/30
    up ip route replace {ATTACK_NET} via 10.0.1.2 dev {router["vpn_interface"]}
    up ip route replace {ADMIN_NET} via 10.0.1.2 dev {router["vpn_interface"]}

auto {router["forcad_interface"]}
iface {router["forcad_interface"]} inet static
    address 10.0.2.1/24

auto {router["team_trunk"]}
iface {router["team_trunk"]} inet manual
"""
    )
    for team in teams(config):
        result += f"\nauto {team['interface']}\niface {team['interface']} inet static\n    address {team['gateway']}/24\n    vlan-raw-device {router['team_trunk']}\n"
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
    capture_filter = f"ip and net {ATTACK_NET} and ({target_filter})"
    return {
        "router/game-metrics.zeek": (ROOT / "router/game-metrics.zeek").read_text(),
        "router/game-inventory.zeek": (
            "# Public mapping; keep synchronized with WireGuard and ForcAD.\n"
            f"redef Forcad::peers = {{\n{peer_map}\n}};\n"
            f"redef Forcad::vulnboxes = {{ {targets} }};\n"
            f"redef Forcad::game_ports = {{ {ports} }};\n"
            'redef Telemetry::metrics_address = "10.0.2.1";\n'
            "redef Telemetry::metrics_port = 9911/tcp;\n"
            'redef Telemetry::metrics_endpoint_name = "router";\n'
        ),
        "router/game-metrics.service": template(
            "router/game-metrics.service.in",
            INTERFACE=config["router"]["vpn_interface"],
            FILTER=capture_filter,
        ),
        "forcad/game-sensors.yml": '- targets: ["10.0.2.1:9911"]\n',
    }


def render(config):
    files = {}
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
    for name in ("network-metrics.service", "network-rules.service"):
        files[f"router/{name}"] = (ROOT / "router" / name).read_text()
    files["vpn/firewall.nft"] = vpn_rules(config)
    files["vpn/firewall.conf"] = (ROOT / "vpn/firewall.conf").read_text()
    team_yaml = "# Merge this teams section into config/config.yml; preserve existing tokens.\nteams:\n"
    for team in teams(config):
        team_yaml += f"""  - name: {team["name"]}
    ip: {team["vulnbox"]}
    highlighted: false
    token: null
    traffic_cidrs:
      - {team["vulnbox"]}/32
      - {team["vpn_cidr"]}
"""
    files["forcad/teams.yml"] = team_yaml
    files[
        "forcad/environment.env"
    ] = """# Merge into deployment/docker/.env; do not replace existing branding settings.
FORCAD_NGINX_BIND=10.0.2.10
PUBLIC_ORIGIN=http://10.0.2.10
PUBLIC_SCHEME=http
"""
    files["forcad/routers.yml"] = '- targets: ["10.0.2.1:9109"]\n'
    files.update(game_metrics(config))
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=ROOT / "network.toml")
    parser.add_argument("--output", type=Path, default=ROOT / "runtime")
    args = parser.parse_args()
    try:
        config = load_config(args.config)
        files = render(config)
        files["network.toml"] = args.config.read_text()
        write_bundle(args.output, files)
    except (ValueError, TypeError, OSError, tomllib.TOMLDecodeError) as error:
        parser.exit(1, f"Render failed: {error}\n")
    print(
        f"Rendered {config['teams']} teams into {args.output}. No host configuration was applied."
    )


if __name__ == "__main__":
    main()

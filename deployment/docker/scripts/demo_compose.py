"""Build the demo's team services from the rendered inventory."""

import copy
import ipaddress

import yaml


class ComposeLoader(yaml.SafeLoader):
    pass


def reset_value(loader, node):
    if isinstance(node, yaml.SequenceNode):
        return loader.construct_sequence(node)
    return loader.construct_mapping(node)


ComposeLoader.add_constructor("!reset", reset_value)


def compose_teams(root, inventory):
    # Read shared container defaults; !reset tags only occur on base services.
    source = yaml.load(
        (root / "deployment/docker/compose.demo.yml").read_text(),
        Loader=ComposeLoader,
    )
    services = {}
    networks = {}
    volumes = {}
    router_networks = {}
    for index, team in enumerate(inventory):
        number = team["id"]
        peer = inventory[(index + 1) % len(inventory)]["id"]
        network = f"demo-team{number}"
        player = f"demo-player{number}"
        farm = f"demo-farm{number}"
        subnet = ipaddress.IPv4Network(team["network"])
        networks[network] = {
            "internal": True,
            "ipam": {
                "config": [
                    {
                        "subnet": str(subnet),
                        "gateway": str(subnet.broadcast_address - 1),
                    }
                ]
            },
        }
        router_networks[network] = {
            "ipv4_address": team["gateway"],
            "interface_name": team["interface"],
        }
        volume = f"{network}-data"
        volumes[volume] = {}
        services[network] = {
            **copy.deepcopy(source["x-team"]),
            "environment": {"DEMO_GATEWAY": team["gateway"]},
            "volumes": [
                f"{volume}:/data",
                f"../../config/runtime/demo/seeds/team{number}:/app/seed:ro",
            ],
            "networks": {
                network: {"ipv4_address": team["vulnbox"], "interface_name": "eth0"}
            },
        }
        services[player] = {
            **copy.deepcopy(source["x-player"]),
            "networks": {
                "demo-wan": {
                    "ipv4_address": f"172.30.241.{10 + number}",
                    "interface_name": "eth0",
                }
            },
            "volumes": [
                f"../../config/runtime/demo/players/team{number}.conf:/app/keys/wg0.conf:ro"
            ],
        }
        services[farm] = {
            **copy.deepcopy(source["x-demo"]),
            "command": ["traffic"],
            "user": "10001:10001",
            "network_mode": f"service:{player}",
            "env_file": f"../../config/runtime/demo-team{number}.env",
            "depends_on": {
                player: {"condition": "service_healthy"},
                f"demo-team{peer}": {"condition": "service_healthy"},
                "nginx": {"condition": "service_started"},
                "demo-sensor": {"condition": "service_healthy"},
            },
        }
    services["demo-router"] = {"networks": router_networks}
    command = list(source["services"]["demo-sensor"]["command"])
    hosts = " or ".join(f"host {team['vulnbox']}" for team in inventory)
    command[command.index("-f") + 1] = f"ip and net 10.183.200.0/24 and ({hosts})"
    services["demo-sensor"] = {"command": command}
    return yaml.safe_dump(
        {"services": services, "networks": networks, "volumes": volumes},
        sort_keys=False,
    )

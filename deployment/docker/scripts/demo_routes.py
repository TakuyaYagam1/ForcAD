"""Reject demo bridges that would shadow an existing host route."""

import ipaddress
import json
import subprocess
import sys


def check_routes(networks, routes, owned_bridges):
    subnets = [
        ipaddress.ip_network(pool["subnet"])
        for network in networks.values()
        for pool in network.get("ipam", {}).get("config", [])
        if "subnet" in pool
    ]
    for route in routes:
        destination = route.get("dst", "default")
        if destination == "default" or route.get("dev") in owned_bridges:
            continue
        existing = ipaddress.ip_network(destination)
        if existing.prefixlen == 0:
            continue
        for subnet in subnets:
            if subnet.overlaps(existing):
                raise SystemExit(
                    f"Demo subnet {subnet} overlaps host route {existing} "
                    f"({route.get('dev', 'policy route')}); no containers were stopped"
                )


def main():
    networks = json.load(sys.stdin)["networks"]
    label = "label=com.docker.compose.project=forcad-demo"
    ids = subprocess.check_output(
        ["docker", "network", "ls", "-q", "--filter", label],
        text=True,
    ).split()
    owned = set()
    if ids:
        details = subprocess.check_output(["docker", "network", "inspect", *ids])
        for net in json.loads(details):
            if net["Driver"] == "bridge":
                options = net.get("Options") or {}
                bridge = options.get("com.docker.network.bridge.name")
                owned.add(bridge or "br-" + net["Id"][:12])
    routes = json.loads(
        subprocess.check_output(["ip", "-j", "-4", "route", "show", "table", "all"])
    )
    check_routes(networks, routes, owned)


if __name__ == "__main__":
    main()

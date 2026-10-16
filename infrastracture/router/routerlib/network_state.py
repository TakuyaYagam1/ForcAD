"""Helpers for validating and comparing captured router network state."""

import ipaddress
import json
import re


class NetworkState:
    def check_restore(self, baseline, applied, current=None, force=False):
        """Reject uninstall when state changed after this bundle was applied."""
        self._check_snapshot(baseline, match_bundle=False)
        self._check_snapshot(applied, match_bundle=False)
        if baseline["config"] != applied["config"]:
            raise ValueError("Router network scope changed while installed")
        if current is None:
            current = self._snapshot_plan(baseline["config"])
        self._check_snapshot(current, match_bundle=False)
        if current["config"] != baseline["config"]:
            raise ValueError("Current router network scope differs from snapshot")

        plan = baseline["config"]
        for name, state in baseline["links"].items():
            if state is None and applied["links"].get(name) is not None:
                expected = applied["links"][name]
                actual = current["links"].get(name)
                if actual != expected:
                    self._drift(force, f"Created VLAN link changed: {name}")
                if actual is not None:
                    if current["addresses4"].get(name, []) != applied["addresses4"].get(
                        name, []
                    ):
                        raise ValueError(
                            f"Created VLAN has foreign IPv4 addresses: {name}"
                        )
                    if not self._route_sets_equal(
                        current["routes4_by_device"].get(name, []),
                        applied["routes4_by_device"].get(name, []),
                    ):
                        raise ValueError(
                            f"Created VLAN has foreign IPv4 routes: {name}"
                        )

        for name, before in baseline["links"].items():
            after = applied["links"].get(name)
            now = current["links"].get(name)
            if (
                before is not None
                and after is not None
                and before["up"] != after["up"]
                and (now is None or now["up"] != after["up"])
            ):
                self._drift(force, f"Managed link state changed: {name}")

        for name, cidr in plan["addresses"]:
            before = cidr in baseline["addresses4"].get(name, [])
            after = cidr in applied["addresses4"].get(name, [])
            if not before and after and cidr not in current["addresses4"].get(name, []):
                self._drift(force, f"Applied IPv4 address changed: {name} {cidr}")

        for dst, before in baseline["routes4"].items():
            after = applied["routes4"].get(dst, [])
            now = current["routes4"].get(dst, [])
            if not self._route_sets_equal(before, after) and not self._route_sets_equal(
                now, after
            ):
                self._drift(force, f"Applied IPv4 route changed: {dst}")

        for key, before in baseline["sysctls"].items():
            after = applied["sysctls"].get(key)
            if before != after and current["sysctls"].get(key) != after:
                self._drift(force, f"Applied network sysctl changed: {key}")

        device = plan["dns"]["device"]
        if (
            baseline["dns_record"] != applied["dns_record"]
            and current["dns_record"] != applied["dns_record"]
        ):
            self._drift(force, f"Applied WAN DNS record changed: {device}")

    def _check_restore_safety(self, snapshot, current, applied, allowed_current=None):
        plan = snapshot["config"]
        # Refuse to delete or recreate state when another interface now owns
        # an address that the saved state would need.
        self._check_addresses(snapshot)
        for name, state in snapshot["links"].items():
            if (
                state is not None
                and current["links"].get(name) is None
                and name not in plan["vlans"]
            ):
                raise ValueError(f"Saved network device disappeared: {name}")
        for name in plan["vlans"]:
            if snapshot["links"].get(name) is not None:
                continue
            actual = current["links"].get(name)
            if actual is None:
                continue
            parent, vlan_id = plan["vlans"][name]
            if (
                actual["kind"] != "vlan"
                or actual["parent"] != parent
                or actual["vlan_id"] != vlan_id
            ):
                raise ValueError(f"Refusing to change a foreign VLAN device: {name}")
            expected_addresses = {
                cidr for device, cidr in plan["addresses"] if device == name
            }
            if set(current["addresses4"].get(name, [])) - expected_addresses:
                raise ValueError(f"Created VLAN has foreign IPv4 addresses: {name}")
            expected_networks = {
                str(ipaddress.ip_interface(cidr).network) for cidr in expected_addresses
            }
            seen = set()
            for route in current["routes4_by_device"].get(name, []):
                if (
                    route["dst"] not in expected_networks
                    or route.get("gateway")
                    or route.get("protocol") not in (None, "kernel")
                    or route.get("scope") not in (None, "link")
                    or route["dst"] in seen
                ):
                    raise ValueError(f"Created VLAN has foreign IPv4 routes: {name}")
                seen.add(route["dst"])

        for dst, original in snapshot["routes4"].items():
            if applied is not None and self._route_sets_equal(
                original, applied["routes4"].get(dst, [])
            ):
                continue
            now = current["routes4"].get(dst, [])
            if self._route_sets_equal(now, original):
                continue
            route = next(item for item in plan["routes"] if item["dst"] == dst)
            allowed_routes = (
                allowed_current["routes4"].get(dst, [])
                if allowed_current is not None
                else None
            )
            if allowed_routes is not None and self._route_sets_equal(
                now, allowed_routes
            ):
                if now and (len(now) != 1 or now[0]["dev"] != route["dev"]):
                    raise ValueError(f"Cannot safely restore changed route: {dst}")
                continue
            if now and (len(now) != 1 or not self._route_matches(now[0], route)):
                raise ValueError(f"Cannot safely restore changed route: {dst}")

        if applied is None:
            expected_dns = plan["dns"]["record"]
            allowed_dns = (
                allowed_current["dns_record"] if allowed_current is not None else None
            )
            if current["dns_record"] not in (
                snapshot["dns_record"],
                expected_dns,
                allowed_dns,
            ):
                raise ValueError("Cannot safely restore changed WAN DNS record")

    def _check_snapshot(self, snapshot, match_bundle=True):
        if not isinstance(snapshot, dict) or snapshot.get("version") != 1:
            raise ValueError("Unsupported router network snapshot")
        required = {
            "config",
            "links",
            "addresses4",
            "routes4",
            "routes4_by_device",
            "ipv6_addresses",
            "sysctls",
            "dns_record",
        }
        if not required <= set(snapshot):
            raise ValueError("Incomplete router network snapshot")
        if match_bundle and self._plan() != snapshot["config"]:
            raise ValueError("Router network config changed after snapshot")
        self._validate_plan(snapshot["config"])
        if not isinstance(snapshot["links"], dict) or set(snapshot["links"]) != set(
            snapshot["config"]["managed"]
        ):
            raise ValueError("Invalid router network snapshot links")
        for state in snapshot["links"].values():
            if state is not None and (
                not isinstance(state, dict)
                or set(state) != {"up", "kind", "parent", "vlan_id"}
                or not isinstance(state["up"], bool)
            ):
                raise ValueError("Invalid router network snapshot link state")
        self._check_vlans(snapshot["config"], snapshot["links"])

    def _validate_plan(self, plan):
        if not isinstance(plan, dict) or set(plan) != {
            "devices",
            "managed",
            "addresses",
            "routes",
            "vlans",
            "dns",
        }:
            raise ValueError("Invalid router network snapshot config")
        devices = self._names(plan["devices"], "devices")
        addresses = []
        for item in plan["addresses"]:
            if not isinstance(item, list) or len(item) != 2:
                raise ValueError("Invalid router network snapshot address")
            name = self._name(item[0])
            address = ipaddress.ip_interface(item[1])
            if address.version != 4:
                raise ValueError("Router network supports static IPv4 addresses only")
            addresses.append([name, str(address)])
        if not addresses or len({name for name, _ in addresses}) != len(addresses):
            raise ValueError("Invalid router network snapshot addresses")
        routes = []
        for item in plan["routes"]:
            if not isinstance(item, dict) or set(item) != {"dst", "gateway", "dev"}:
                raise ValueError("Invalid router network snapshot route")
            routes.append(
                {
                    "dst": self._route_dst(item),
                    "gateway": str(ipaddress.IPv4Address(item["gateway"])),
                    "dev": self._name(item["dev"]),
                }
            )
        if not routes or len({route["dst"] for route in routes}) != len(routes):
            raise ValueError("Invalid router network snapshot routes")
        if any(route["dev"] not in devices for route in routes):
            raise ValueError("Router route device is not bundle-owned")
        expected_addresses = sorted(addresses)
        expected_routes = sorted(routes, key=lambda route: route["dst"])
        managed = sorted(set(devices) | {name for name, _ in addresses})
        vlans = {}
        for name in set(managed) - set(devices):
            if "." not in name:
                raise ValueError(f"Address device is not in bundle devices: {name}")
            parent, raw_id = name.rsplit(".", 1)
            if (
                parent not in devices
                or not raw_id.isdigit()
                or not 1 <= int(raw_id) <= 4094
            ):
                raise ValueError(f"Invalid generated VLAN device: {name}")
            vlans[name] = [parent, int(raw_id)]
        wan = next(
            (route["dev"] for route in routes if route["dst"] == "default"), None
        )
        dns = plan["dns"]
        if (
            not wan
            or not isinstance(dns, dict)
            or set(dns) != {"device", "record"}
            or dns["device"] != wan
            or not isinstance(dns["record"], str)
        ):
            raise ValueError("Invalid router network snapshot DNS record")
        nameservers = []
        for line in dns["record"].splitlines():
            fields = line.split()
            if len(fields) != 2 or fields[0] != "nameserver":
                raise ValueError("Invalid router network snapshot DNS record")
            nameservers.append(str(ipaddress.IPv4Address(fields[1])))
        if not nameservers or dns["record"] != "".join(
            f"nameserver {value}\n" for value in nameservers
        ):
            raise ValueError("Invalid router network snapshot DNS record")
        expected = {
            "devices": sorted(devices),
            "managed": managed,
            "addresses": expected_addresses,
            "routes": expected_routes,
            "vlans": vlans,
            "dns": dns,
        }
        if plan != expected:
            raise ValueError("Noncanonical router network snapshot config")

    def _drift(self, force, message):
        if not force:
            raise ValueError(message)

    def _check_snapshot_current(self, snapshot):
        plan = snapshot["config"]
        links = {
            item["ifname"]: item
            for item in self._json("ip", "-j", "-d", "link", "show")
        }
        for name, original in snapshot["links"].items():
            current = links.get(name)
            if original is None:
                if current is not None:
                    raise ValueError(f"Network link appeared after snapshot: {name}")
            elif not current or self._link_state(current) != original:
                raise ValueError(f"Network link changed after snapshot: {name}")
        addresses = self._json("ip", "-j", "-4", "address", "show")
        for name, original in snapshot["addresses4"].items():
            if self._ipv4_addresses(addresses, name) != original:
                raise ValueError(f"IPv4 addresses changed after snapshot: {name}")
        for dst, original in snapshot["routes4"].items():
            if not self._route_sets_equal(self._routes_for(dst), original):
                raise ValueError(f"IPv4 route changed after snapshot: {dst}")
        if self._read_sysctls() != snapshot["sysctls"]:
            raise ValueError("Network sysctls changed after snapshot")
        if self._read_dns_record(plan["dns"]["device"]) != snapshot["dns_record"]:
            raise ValueError("WAN DNS record changed after snapshot")

    def _check_addresses(self, snapshot):
        plan = snapshot["config"]
        addresses = self._json("ip", "-j", "-4", "address", "show")
        desired = {(name, cidr) for name, cidr in plan["addresses"]}
        desired_locals = {
            str(ipaddress.ip_interface(cidr).ip): name
            for name, cidr in plan["addresses"]
        }
        for device in addresses:
            name = device["ifname"]
            for info in device.get("addr_info", []):
                if info.get("family") != "inet" or not info.get("local"):
                    continue
                local = info["local"]
                owner = desired_locals.get(local)
                if owner and (name, f"{local}/{info['prefixlen']}") not in desired:
                    raise ValueError(
                        f"Target IPv4 address {local} is already assigned to {name}"
                    )
                if (
                    name in plan["managed"]
                    and owner == name
                    and f"{local}/{info['prefixlen']}"
                    not in {
                        cidr for device_name, cidr in desired if device_name == name
                    }
                ):
                    raise ValueError(
                        f"Conflicting prefix for managed address {local} on {name}"
                    )

    def _check_routes(self, plan):
        routes = self._json("ip", "-j", "-4", "route", "show", "table", "main")
        for wanted in plan["routes"]:
            same = [item for item in routes if self._route_dst(item) == wanted["dst"]]
            if len(same) > 1:
                raise ValueError(
                    f"Multiple routes make this destination ambiguous: {wanted['dst']}"
                )
            if same:
                route = same[0]
                if route.get("dev") != wanted["dev"]:
                    raise ValueError(
                        f"Target route {wanted['dst']} is owned by another device"
                    )
                if not route.get("gateway"):
                    raise ValueError(
                        f"Target route {wanted['dst']} conflicts with a connected route"
                    )
                self._route_state(route)
            if wanted["dst"] != "default":
                target = ipaddress.ip_network(wanted["dst"])
                for other in routes:
                    dst = self._route_dst(other)
                    if (
                        dst != "default"
                        and dst != wanted["dst"]
                        and ipaddress.ip_network(dst).overlaps(target)
                    ):
                        raise ValueError(
                            f"Overlapping IPv4 route conflicts with {wanted['dst']}"
                        )

    def _preserved_ipv6_addresses(self, devices):
        result = []
        for device in devices:
            for info in device.get("addr_info", []):
                local = info.get("local")
                scope = self._preserved_v6_scope(local) if local else None
                if scope:
                    result.append(
                        [device["ifname"], f"{local}/{info['prefixlen']}", scope]
                    )
        return sorted(result)

    def _preserved_v6_scope(self, value):
        address = ipaddress.IPv6Address(value)
        if address.is_link_local:
            return "link"
        if address.is_loopback:
            return "host"
        return None

    def _route_args(self, operation, route):
        state = self._route_state(route)
        args = [operation, state["dst"]]
        if state.get("gateway"):
            args += ["via", state["gateway"]]
        args += ["dev", state["dev"]]
        for key, option in (
            ("protocol", "proto"),
            ("scope", "scope"),
            ("prefsrc", "src"),
            ("metric", "metric"),
        ):
            if state.get(key) is not None:
                args += [option, str(state[key])]
        if state.get("onlink"):
            args.append("onlink")
        return args

    def _route_state(self, route):
        allowed = {
            "dst",
            "gateway",
            "dev",
            "protocol",
            "scope",
            "prefsrc",
            "metric",
            "table",
            "type",
            "flags",
            "onlink",
        }
        flags = route.get("flags", [])
        if isinstance(flags, str):
            flags = [flags]
        if not isinstance(flags, list) or any(
            value not in {"onlink", "linkdown"} for value in flags
        ):
            raise ValueError("Complex IPv4 route cannot be restored safely")
        if set(route) - allowed or route.get("type", "unicast") != "unicast":
            raise ValueError("Complex IPv4 route cannot be restored safely")
        state = {"dst": self._route_dst(route)}
        if route.get("gateway"):
            state["gateway"] = str(ipaddress.IPv4Address(route["gateway"]))
        if route.get("dev"):
            state["dev"] = self._name(route["dev"])
        else:
            raise ValueError("IPv4 route without a device cannot be restored safely")
        if route.get("table") not in (None, "main", 254):
            raise ValueError("Non-main IPv4 route cannot be restored safely")
        for key in ("protocol", "scope"):
            if route.get(key) is not None:
                value = str(route[key])
                if not re.fullmatch(r"[A-Za-z0-9_-]+", value):
                    raise ValueError("Invalid IPv4 route attribute")
                state[key] = value
        if route.get("prefsrc"):
            state["prefsrc"] = str(ipaddress.IPv4Address(route["prefsrc"]))
        if route.get("metric") is not None:
            if type(route["metric"]) is not int or route["metric"] < 0:
                raise ValueError("Invalid IPv4 route metric")
            state["metric"] = route["metric"]
        onlink_value = route.get("onlink", False)
        if not isinstance(onlink_value, bool):
            raise TypeError("Invalid IPv4 route onlink flag")
        onlink = onlink_value or "onlink" in flags
        if onlink:
            state["onlink"] = True
        return state

    def _route_matches(self, actual, wanted):
        try:
            state = self._route_state(actual)
        except ValueError:
            return False
        return (
            state["dst"] == wanted["dst"]
            and state.get("gateway") == wanted["gateway"]
            and state["dev"] == wanted["dev"]
            and state.get("metric", 0) == 0
            and "prefsrc" not in state
        )

    def _route_sets_equal(self, left, right):
        try:
            a = sorted(
                (self._route_state(item) for item in left),
                key=lambda item: json.dumps(item, sort_keys=True),
            )
            b = sorted(
                (self._route_state(item) for item in right),
                key=lambda item: json.dumps(item, sort_keys=True),
            )
            return a == b
        except ValueError:
            return False

    def _routes_for(self, dst):
        routes = self._json("ip", "-j", "-4", "route", "show", "table", "main")
        return [
            self._route_state(item) for item in routes if self._route_dst(item) == dst
        ]

    def _route_dst(self, route):
        raw = route.get("dst", "default")
        if raw == "default":
            return "default"
        network = ipaddress.ip_network(raw, strict=False)
        if network.version != 4:
            raise ValueError("Router routes must use IPv4")
        return str(network)

    def _link_state(self, item):
        info = item.get("linkinfo", {})
        data = info.get("info_data", {})
        return {
            "up": "UP" in item.get("flags", []),
            "kind": info.get("info_kind"),
            "parent": item.get("link"),
            "vlan_id": data.get("id"),
        }

    def _check_vlans(self, plan, states):
        for name, (parent, vlan_id) in plan["vlans"].items():
            state = states.get(name)
            if state is not None and (
                state["kind"] != "vlan"
                or state["parent"] != parent
                or state["vlan_id"] != vlan_id
            ):
                raise ValueError(f"Existing VLAN device does not match bundle: {name}")

    def _ipv4_addresses(self, devices, name):
        result = []
        for device in devices:
            if device.get("ifname") != name:
                continue
            for address in device.get("addr_info", []):
                if address.get("family") == "inet" and address.get("local"):
                    result.append(f"{address['local']}/{address['prefixlen']}")
        return sorted(set(result))

    def _global_v6(self, value):
        address = ipaddress.IPv6Address(value)
        return not (
            address.is_link_local
            or address.is_loopback
            or address.is_multicast
            or address.is_unspecified
        )

    def _global_v6_network(self, value):
        if value == "default":
            return True
        network = ipaddress.IPv6Network(value, strict=False)
        address = network.network_address
        return not (
            address.is_link_local
            or address.is_loopback
            or address.is_multicast
            or address.is_unspecified
        )

    def _name(self, value):
        if not isinstance(value, str) or not re.fullmatch(
            r"[A-Za-z][A-Za-z0-9_.-]{0,14}", value
        ):
            raise ValueError("Invalid Linux network device name")
        return value

    def _names(self, values, location):
        if not isinstance(values, list) or not values:
            raise ValueError(f"Router bundle has invalid {location}")
        names = [self._name(value) for value in values]
        if len(names) != len(set(names)):
            raise ValueError(f"Router bundle has duplicate {location}")
        return names

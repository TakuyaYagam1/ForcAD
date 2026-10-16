"""Apply and restore the router's scoped IPv4 network state."""

import glob
import ipaddress
import json
import os
import re
import shlex
import shutil
import subprocess
from pathlib import Path

from .network_state import NetworkState


class Network(NetworkState):
    """Manage only addresses, routes, links, DNS and sysctls owned by a bundle."""

    def __init__(self, bundle):
        self.bundle = bundle

    def snapshot(self, config=None):
        plan = self._plan() if config is None else config
        if config is not None:
            self._validate_plan(plan)
        return self._snapshot_plan(plan)

    def _snapshot_plan(self, plan):
        links = self._json("ip", "-j", "-d", "link", "show")
        link_by_name = {item["ifname"]: item for item in links}
        states = {}
        for name in plan["managed"]:
            item = link_by_name.get(name)
            if item is None and name in plan["devices"]:
                raise ValueError(f"Network device does not exist: {name}")
            states[name] = self._link_state(item) if item else None
        self._check_vlans(plan, states)

        addresses = self._json("ip", "-j", "-4", "address", "show")
        before_addresses = {
            name: self._ipv4_addresses(addresses, name) for name in plan["managed"]
        }
        routes = self._json("ip", "-j", "-4", "route", "show", "table", "main")
        before_routes = {
            route["dst"]: [
                self._route_state(item)
                for item in routes
                if self._route_dst(item) == route["dst"]
            ]
            for route in plan["routes"]
        }
        routes_by_device = {
            name: sorted(
                (self._route_state(item) for item in routes if item.get("dev") == name),
                key=lambda item: json.dumps(item, sort_keys=True),
            )
            for name in plan["managed"]
        }
        ipv6_addresses = self._json("ip", "-j", "-6", "address", "show")
        preserved_ipv6 = self._preserved_ipv6_addresses(ipv6_addresses)
        sysctls = self._read_sysctls()
        dns_record = self._read_dns_record(plan["dns"]["device"])
        return {
            "version": 1,
            "config": plan,
            "links": states,
            "addresses4": before_addresses,
            "routes4": before_routes,
            "routes4_by_device": routes_by_device,
            "ipv6_addresses": preserved_ipv6,
            "sysctls": sysctls,
            "dns_record": dns_record,
        }

    def preflight(self, snapshot):
        self._check_snapshot(snapshot)
        plan = snapshot["config"]
        self._check_snapshot_current(snapshot)
        self._check_interfaces(plan)
        self._check_other_managers()
        self._check_ipv6()
        self._check_addresses(snapshot)
        self._check_routes(plan)
        self._check_resolvconf(plan)
        self._check_sysctl_file()

    def apply(self, snapshot):
        self._require_live_root()
        self.preflight(snapshot)
        plan = snapshot["config"]
        current_links = {
            item["ifname"]: item
            for item in self._json("ip", "-j", "-d", "link", "show")
        }
        for name, (parent, vlan_id) in plan["vlans"].items():
            if name not in current_links:
                self._run(
                    "ip",
                    "link",
                    "add",
                    "link",
                    parent,
                    "name",
                    name,
                    "type",
                    "vlan",
                    "id",
                    str(vlan_id),
                )
        current_links = {
            item["ifname"]: item
            for item in self._json("ip", "-j", "-d", "link", "show")
        }
        for name in plan["managed"]:
            if "UP" not in current_links[name].get("flags", []):
                self._run("ip", "link", "set", "dev", name, "up")

        current_addresses = self._json("ip", "-j", "-4", "address", "show")
        for name, cidr in plan["addresses"]:
            if cidr not in self._ipv4_addresses(current_addresses, name):
                self._run("ip", "-4", "address", "add", cidr, "dev", name)

        current_routes = self._json("ip", "-j", "-4", "route", "show", "table", "main")
        for route in plan["routes"]:
            existing = [
                item for item in current_routes if self._route_dst(item) == route["dst"]
            ]
            if existing and self._route_matches(existing[0], route):
                continue
            if existing:
                self._run("ip", "-4", "route", *self._route_args("del", existing[0]))
            self._run("ip", "-4", "route", *self._route_args("add", route))
            current_routes = self._json(
                "ip", "-j", "-4", "route", "show", "table", "main"
            )

        sysctl_file = self._installed_sysctl_file()
        if not self._sysctls_match(snapshot, self._wanted_sysctls(sysctl_file)):
            self._run("sysctl", "-p", str(sysctl_file))
        name = plan["dns"]["device"] + ".inet"
        if self._read_dns_record(plan["dns"]["device"]) != plan["dns"]["record"]:
            self._run("resolvconf", "-a", name, input_text=plan["dns"]["record"])

    def restore(self, snapshot, applied=None, force=False, allowed_current=None):
        self._require_live_root()
        self._check_snapshot(snapshot, match_bundle=False)
        plan = snapshot["config"]
        if allowed_current is not None:
            self._check_snapshot(allowed_current, match_bundle=False)
            if allowed_current["config"] != plan:
                raise ValueError(
                    "Allowed recovery snapshot has a different network scope"
                )
        current = self._snapshot_plan(plan)
        if applied is not None:
            self.check_restore(snapshot, applied, current=current, force=force)
        self._check_restore_safety(snapshot, current, applied, allowed_current)

        # Recreate only VLANs present in the saved state. The parent and VLAN
        # ID come from the validated journaled plan, never from live link data.
        for name, state in snapshot["links"].items():
            if state is not None and current["links"].get(name) is None:
                if name not in plan["vlans"]:
                    raise ValueError(f"Saved network device disappeared: {name}")
                parent, vlan_id = plan["vlans"][name]
                self._run(
                    "ip",
                    "link",
                    "add",
                    "link",
                    parent,
                    "name",
                    name,
                    "type",
                    "vlan",
                    "id",
                    str(vlan_id),
                )

        current_addresses = self._json("ip", "-j", "-4", "address", "show")
        if applied is None:
            # Only restore bundle addresses removed by a partial uninstall.
            for name, cidr in plan["addresses"]:
                if cidr in snapshot["addresses4"].get(
                    name, []
                ) and cidr not in self._ipv4_addresses(current_addresses, name):
                    self._run("ip", "-4", "address", "add", cidr, "dev", name)
        for name, cidr in plan["addresses"]:
            introduced = cidr not in snapshot["addresses4"].get(name, [])
            if applied is not None:
                introduced = introduced and cidr in applied["addresses4"].get(name, [])
            if introduced and cidr in self._ipv4_addresses(current_addresses, name):
                self._run("ip", "-4", "address", "del", cidr, "dev", name)

        for route in plan["routes"]:
            if applied is not None and self._route_sets_equal(
                snapshot["routes4"].get(route["dst"], []),
                applied["routes4"].get(route["dst"], []),
            ):
                continue
            original = snapshot["routes4"].get(route["dst"], [])
            current = self._routes_for(route["dst"])
            if self._route_sets_equal(current, original):
                continue
            allowed_routes = (
                allowed_current["routes4"].get(route["dst"], [])
                if allowed_current is not None
                else None
            )
            allowed_match = allowed_routes is not None and self._route_sets_equal(
                current, allowed_routes
            )
            if (
                current
                and not allowed_match
                and not (len(current) == 1 and self._route_matches(current[0], route))
            ):
                raise ValueError(f"Cannot safely restore changed route: {route['dst']}")
            if current:
                self._run("ip", "-4", "route", *self._route_args("del", current[0]))
            for old in original:
                self._run("ip", "-4", "route", *self._route_args("add", old))

        for name in plan["vlans"]:
            created = snapshot["links"].get(name) is None
            if applied is not None:
                created = created and applied["links"].get(name) is not None
            if created and self._link_exists(name):
                self._run("ip", "link", "delete", "dev", name)

        sysctls = snapshot["sysctls"]
        if applied is not None:
            sysctls = {
                key: value
                for key, value in snapshot["sysctls"].items()
                if applied["sysctls"].get(key) != value
            }
        self._restore_sysctls(sysctls)
        for name, state in snapshot["links"].items():
            if state is None:
                continue
            if applied is not None and state == applied["links"].get(name):
                continue
            desired_up = state["up"]
            present = self._link_by_name(name)
            if present and ("UP" in present.get("flags", [])) != desired_up:
                self._run(
                    "ip", "link", "set", "dev", name, "up" if desired_up else "down"
                )
        if applied is None or snapshot["ipv6_addresses"] != applied["ipv6_addresses"]:
            self._restore_ipv6_addresses(snapshot["ipv6_addresses"])
        if applied is None or snapshot["dns_record"] != applied["dns_record"]:
            self._restore_dns(plan["dns"]["device"], snapshot["dns_record"])

    def verify(self):
        plan = self._plan()
        links = {
            item["ifname"]: item for item in self._json("ip", "-j", "link", "show")
        }
        for name in plan["managed"]:
            if name not in links or "UP" not in links[name].get("flags", []):
                raise ValueError(f"Network device is down: {name}")
        addresses = self._json("ip", "-j", "-4", "address", "show")
        for name, cidr in plan["addresses"]:
            if cidr not in self._ipv4_addresses(addresses, name):
                raise ValueError(f"Missing live address: {name} {cidr}")
        routes = self._json("ip", "-j", "-4", "route", "show", "table", "main")
        for expected in plan["routes"]:
            if not any(self._route_matches(item, expected) for item in routes):
                raise ValueError(f"Missing live route: {expected}")
        self._check_sysctls_match(self._wanted_sysctls(self._bundle_sysctl_file()))
        if self._read_dns_record(plan["dns"]["device"]) != plan["dns"]["record"]:
            raise ValueError("Live WAN DNS record differs from this bundle")

    def _plan(self):
        config = self.bundle.config
        devices = self._names(config.get("devices"), "devices")
        addresses = []
        address_names = set()
        for item in config.get("addresses", []):
            if not isinstance(item, list) or len(item) != 2:
                raise ValueError("Invalid router address inventory")
            name = self._name(item[0])
            address = ipaddress.ip_interface(item[1])
            if address.version != 4:
                raise ValueError("Router network supports static IPv4 addresses only")
            addresses.append([name, str(address)])
            address_names.add(name)
        routes = []
        for item in config.get("routes", []):
            if not isinstance(item, dict) or set(item) != {"dst", "gateway", "dev"}:
                raise ValueError("Invalid router route inventory")
            dst = self._route_dst(item)
            gateway = str(ipaddress.IPv4Address(item["gateway"]))
            dev = self._name(item["dev"])
            routes.append({"dst": dst, "gateway": gateway, "dev": dev})
        if not addresses or not routes or not devices:
            raise ValueError("Router bundle must define devices, addresses and routes")
        if len({name for name, _ in addresses}) != len(addresses):
            raise ValueError("Router bundle has duplicate address devices")
        if len({route["dst"] for route in routes}) != len(routes):
            raise ValueError("Router bundle has duplicate route destinations")
        if any(route["dev"] not in devices for route in routes):
            raise ValueError("Router route device is not bundle-owned")
        managed = sorted(set(devices) | address_names)
        vlans = {}
        for name in address_names - set(devices):
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
        dns = self._generated_dns(routes)
        return {
            "devices": sorted(devices),
            "managed": managed,
            "addresses": sorted(addresses),
            "routes": sorted(routes, key=lambda route: route["dst"]),
            "vlans": vlans,
            "dns": dns,
        }

    def _generated_dns(self, routes):
        wan = next(
            (route["dev"] for route in routes if route["dst"] == "default"), None
        )
        if not wan:
            raise ValueError("Router bundle must define a WAN default route")
        path = self.bundle.directory / "interfaces"
        if path.is_symlink() or not path.is_file():
            raise ValueError("Router bundle is missing generated interfaces")
        current = None
        servers = []
        for line in path.read_text().splitlines():
            fields = shlex.split(line, comments=True)
            if not fields:
                continue
            if fields[0] == "iface" and len(fields) >= 4:
                current = (fields[1], fields[2])
            elif current == (wan, "inet") and fields[0] == "dns-nameservers":
                servers.extend(fields[1:])
        normalized = [str(ipaddress.IPv4Address(value)) for value in servers]
        if not normalized:
            raise ValueError("Generated WAN interface has no DNS servers")
        return {
            "device": wan,
            "record": "".join(f"nameserver {server}\n" for server in normalized),
        }

    def _check_interfaces(self, plan):
        path = self._root_path("/etc/network/interfaces")
        if path.is_symlink():
            raise ValueError("Refusing symlinked /etc/network/interfaces")
        if not path.exists():
            return
        allowed = {
            tuple(fields)
            for route in plan["routes"]
            if route["dst"] != "default"
            for fields in (
                [
                    "up",
                    "ip",
                    "route",
                    "replace",
                    route["dst"],
                    "via",
                    route["gateway"],
                    "dev",
                    route["dev"],
                ],
                [
                    "down",
                    "ip",
                    "route",
                    "del",
                    route["dst"],
                    "via",
                    route["gateway"],
                    "dev",
                    route["dev"],
                    "||",
                    "true",
                ],
            )
        }
        managed = set(plan["managed"]) | {"lo"}
        self._check_interface_file(path, managed, allowed, set())

    def _check_interface_file(self, path, managed, allowed_hooks, seen):
        path = Path(path)
        if path in seen:
            return
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"Unsafe ifupdown include: {path}")
        seen.add(path)
        current = None
        for raw in path.read_text().splitlines():
            fields = shlex.split(raw, comments=True)
            if not fields:
                continue
            key = fields[0]
            if key in {"source", "source-directory"}:
                for pattern in fields[1:]:
                    expanded = pattern
                    if pattern.startswith("/"):
                        expanded = str(self._root_path(pattern))
                    else:
                        expanded = str(path.parent / pattern)
                    matches = sorted(glob.glob(expanded))
                    if key == "source-directory":
                        matches = sorted(
                            item
                            for item in glob.glob(str(Path(expanded) / "*"))
                            if Path(item).is_file()
                        )
                    for match in matches:
                        self._check_interface_file(
                            Path(match), managed, allowed_hooks, seen
                        )
                current = None
                continue
            if key == "iface":
                if len(fields) != 4 or fields[2] != "inet" or fields[1] not in managed:
                    raise ValueError(
                        "ifupdown must use only managed static IPv4, manual or loopback stanzas"
                    )
                method = fields[3]
                if method not in {"static", "manual"} and not (
                    fields[1] == "lo" and method == "loopback"
                ):
                    raise ValueError(
                        "DHCP and non-IPv4 ifupdown methods are unsupported"
                    )
                current = fields[1]
                continue
            if key in {"auto", "allow-auto", "allow-hotplug"}:
                if any(name not in managed for name in fields[1:]):
                    raise ValueError("ifupdown references an unmanaged network device")
                current = None
                continue
            if current is None:
                raise ValueError(f"Unsupported ifupdown directive: {key}")
            if key in {"up", "down"}:
                if tuple(fields) not in allowed_hooks:
                    raise ValueError(
                        "ifupdown route hooks must match generated ForcAD routes"
                    )
                continue
            if key not in {"address", "gateway", "dns-nameservers", "vlan-raw-device"}:
                raise ValueError(f"Unsupported ifupdown setting: {key}")

    def _check_other_managers(self):
        for service in ("NetworkManager.service", "systemd-networkd.service"):
            result = self._call("systemctl", "is-active", "--quiet", service)
            if result.returncode == 0:
                raise ValueError(
                    f"{service} is active; stop it before applying router networking"
                )
            if result.returncode not in (3, 4):
                raise ValueError(f"Could not determine whether {service} is active")

    def _check_ipv6(self):
        for device in self._json("ip", "-j", "-6", "address", "show"):
            for address in device.get("addr_info", []):
                local = address.get("local")
                if local and self._global_v6(local):
                    raise ValueError(
                        "Global IPv6 addresses exist; generated sysctls disable IPv6"
                    )
        routes = self._json("ip", "-j", "-6", "route", "show", "table", "all")
        for route in routes:
            dst = route.get("dst", "default")
            if self._global_v6_network(dst):
                raise ValueError(
                    "Global IPv6 routes exist; generated sysctls disable IPv6"
                )

    def _check_resolvconf(self, plan):
        if not shutil.which("resolvconf"):
            raise ValueError("Install resolvconf before applying router DNS")
        path = self._root_path("/etc/resolv.conf")
        if not path.is_symlink():
            raise ValueError("/etc/resolv.conf must be managed by resolvconf")
        target = os.path.normpath(os.path.join("/etc", os.readlink(path)))
        if target not in {
            "/run/resolvconf/resolv.conf",
            "/var/run/resolvconf/resolv.conf",
        }:
            raise ValueError("/etc/resolv.conf is not linked to resolvconf")
        self._read_dns_record(plan["dns"]["device"])

    def _check_sysctl_file(self):
        wanted = self._wanted_sysctls(self._bundle_sysctl_file())
        if (
            "net/ipv4/ip_forward" not in wanted
            or "net/ipv6/conf/all/disable_ipv6" not in wanted
        ):
            raise ValueError("Generated router sysctl file is incomplete")

    def _bundle_sysctl_file(self):
        path = self.bundle.directory / "sysctl.conf"
        if path.is_symlink() or not path.is_file():
            raise ValueError("Router bundle is missing generated sysctl.conf")
        return path

    def _installed_sysctl_file(self):
        path = self._root_path("/etc/sysctl.d/90-forcad-router.conf")
        if path.is_symlink() or not path.is_file():
            raise ValueError("Installed router sysctl file is missing")
        if path.read_bytes() != self._bundle_sysctl_file().read_bytes():
            raise ValueError("Installed router sysctl file differs from this bundle")
        return path

    def _wanted_sysctls(self, path):
        result = {}
        for raw in Path(path).read_text().splitlines():
            line = raw.split("#", 1)[0].strip()
            if not line:
                continue
            if "=" not in line:
                raise ValueError("Invalid router sysctl setting")
            key, value = (part.strip() for part in line.split("=", 1))
            key = key.replace(".", "/")
            if (
                not re.fullmatch(r"net/(?:ipv4|ipv6)/[A-Za-z0-9_./-]+", key)
                or not value.isdigit()
            ):
                raise ValueError("Unsupported router sysctl setting")
            result[key] = value
        return result

    def _sysctls_match(self, snapshot, wanted):
        return all(
            snapshot["sysctls"].get(key) == value for key, value in wanted.items()
        )

    def _check_sysctls_match(self, wanted):
        values = self._read_sysctls()
        for key, value in wanted.items():
            if values.get(key) != value:
                raise ValueError(f"Live sysctl differs: {key}")

    def _read_sysctls(self):
        root = self._root_path("/proc/sys")
        values = {}
        directory = root / "net/ipv4/conf"
        if not directory.is_dir():
            raise ValueError("Missing kernel sysctl directory: net/ipv4/conf")
        for path in directory.rglob("*"):
            if path.is_file() and not path.is_symlink():
                values[str(path.relative_to(root))] = path.read_text().strip()
        ipv6 = root / "net/ipv6/conf"
        if not ipv6.is_dir():
            raise ValueError("Missing kernel sysctl directory: net/ipv6/conf")
        for path in ipv6.glob("*/disable_ipv6"):
            if path.is_file() and not path.is_symlink():
                values[str(path.relative_to(root))] = path.read_text().strip()
        forwarding = root / "net/ipv4/ip_forward"
        if not forwarding.is_file():
            raise ValueError("Missing kernel sysctl net/ipv4/ip_forward")
        values["net/ipv4/ip_forward"] = forwarding.read_text().strip()
        if not any(key.endswith("/disable_ipv6") for key in values):
            raise ValueError("Missing IPv6 disable sysctls")
        return dict(sorted(values.items()))

    def _restore_sysctls(self, values):
        current = self._read_sysctls()
        current_after = current
        forward = values.get("net/ipv4/ip_forward")
        if forward is not None and current.get("net/ipv4/ip_forward") != forward:
            self._run("sysctl", "-w", f"net/ipv4/ip_forward={forward}")
            # Changing ip_forward resets IPv4 interface settings. Preserve keys
            # that this installation did not change, then restore changed ones.
            values = dict(values)
            for key, value in current.items():
                if key.startswith("net/ipv4/conf/"):
                    values.setdefault(key, value)
            current_after = self._read_sysctls()
        for prefix in ("net/ipv4/conf/", "net/ipv6/conf/"):
            settings = [
                f"{key}={value}"
                for key, value in values.items()
                if key.startswith(prefix) and current_after.get(key) != value
            ]
            if settings:
                self._run("sysctl", "-w", *settings)

    def _restore_ipv6_addresses(self, saved):
        current = self._json("ip", "-j", "-6", "address", "show")
        have = {
            (item["ifname"], f"{info['local']}/{info['prefixlen']}", info["scope"])
            for item in current
            for info in item.get("addr_info", [])
            if info.get("local") and self._preserved_v6_scope(info["local"])
        }
        links = {item["ifname"] for item in self._json("ip", "-j", "link", "show")}
        for name, cidr, scope in saved:
            if name in links and (name, cidr, scope) not in have:
                self._run(
                    "ip", "-6", "address", "add", cidr, "dev", name, "scope", scope
                )

    def _restore_dns(self, device, original):
        current = self._read_dns_record(device)
        if current == original:
            return
        name = device + ".inet"
        if original is None:
            if current is not None:
                self._run("resolvconf", "-d", name)
        else:
            self._run("resolvconf", "-a", name, input_text=original)

    def _read_dns_record(self, device):
        path = self._root_path(f"/run/resolvconf/interface/{device}.inet")
        if path.is_symlink():
            raise ValueError("Refusing symlinked resolvconf interface record")
        if not path.exists():
            return None
        if not path.is_file():
            raise ValueError("Invalid resolvconf interface record")
        try:
            return path.read_text()
        except UnicodeDecodeError as error:
            raise ValueError("Invalid resolvconf interface record encoding") from error

    def _root_path(self, path):
        return Path(self.bundle.root) / path.lstrip("/")

    def _require_live_root(self):
        if Path(self.bundle.root) != Path("/"):
            raise ValueError("Router network changes require the host root /")

    def _link_by_name(self, name):
        return next(
            (
                item
                for item in self._json("ip", "-j", "link", "show")
                if item["ifname"] == name
            ),
            None,
        )

    def _link_exists(self, name):
        return self._link_by_name(name) is not None

    def _json(self, *args):
        try:
            return json.loads(self._run(*args))
        except json.JSONDecodeError as error:
            raise ValueError(f"Invalid JSON from {' '.join(args)}") from error

    def _run(self, *args, input_text=None):
        result = self._call(*args, input_text=input_text)
        if result.returncode:
            raise ValueError(f"Command failed: {' '.join(map(str, args))}")
        return result.stdout

    def _call(self, *args, input_text=None):
        try:
            return subprocess.run(
                list(map(str, args)),
                capture_output=True,
                text=True,
                input=input_text,
                check=False,
                timeout=30,
            )
        except OSError as error:
            raise ValueError(f"Could not run {args[0]}") from error

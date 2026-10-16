"""Unit tests for scoped router networking, with every host command mocked."""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "router"))

from routerlib import network


class Bundle:
    def __init__(self, root, directory):
        self.root = root
        self.directory = directory
        self.config = {
            "devices": ["wan0", "lan0"],
            "addresses": [
                ["wan0", "192.0.2.2/24"],
                ["lan0", "10.0.0.1/24"],
                ["lan0.101", "10.1.0.1/24"],
            ],
            "routes": [{"dst": "default", "gateway": "192.0.2.1", "dev": "wan0"}],
        }


class FakeCommands:
    def __init__(self, root, *, matched=False, old_gateway=None, ipv6_global=False):
        self.root = root
        self.commands = []
        self.links = [
            {"ifname": "lo", "flags": ["LOOPBACK", "UP"]},
            {"ifname": "wan0", "flags": ["BROADCAST", "UP"]},
            {"ifname": "lan0", "flags": ["BROADCAST", "UP"]},
        ]
        if matched:
            self._add_vlan()
        self.addresses = {"wan0": [], "lan0": [], "lan0.101": []}
        if matched:
            self.addresses = {
                "wan0": ["192.0.2.2/24"],
                "lan0": ["10.0.0.1/24"],
                "lan0.101": ["10.1.0.1/24"],
            }
        self.routes = [
            {
                "dst": "default",
                "gateway": old_gateway or "192.0.2.1",
                "dev": "wan0",
                "flags": ["onlink", "linkdown"],
            }
        ]
        if matched:
            self.routes[0]["protocol"] = "static"
        self.ipv6_global = ipv6_global
        self.ipv6 = self._make_ipv6()

    def _add_vlan(self):
        self.links.append(
            {
                "ifname": "lan0.101",
                "flags": ["BROADCAST", "UP"],
                "link": "lan0",
                "linkinfo": {"info_kind": "vlan", "info_data": {"id": 101}},
            }
        )

    def _all_links(self):
        return json.dumps(self.links)

    def _all_addresses(self):
        result = []
        for name in ("wan0", "lan0", "lan0.101"):
            info = [
                {
                    "family": "inet",
                    "local": cidr.split("/", 1)[0],
                    "prefixlen": int(cidr.split("/", 1)[1]),
                    "scope": "global",
                }
                for cidr in self.addresses.get(name, [])
            ]
            result.append({"ifname": name, "addr_info": info})
        return json.dumps(result)

    def _all_ipv6(self):
        return json.dumps(self.ipv6)

    def _make_ipv6(self):
        result = [
            {
                "ifname": "lo",
                "addr_info": [
                    {
                        "family": "inet6",
                        "local": "::1",
                        "prefixlen": 128,
                        "scope": "host",
                    }
                ],
            },
            {
                "ifname": "wan0",
                "addr_info": [
                    {
                        "family": "inet6",
                        "local": "fe80::1",
                        "prefixlen": 64,
                        "scope": "link",
                    }
                ],
            },
            {
                "ifname": "lan0",
                "addr_info": [
                    {
                        "family": "inet6",
                        "local": "fe80::2",
                        "prefixlen": 64,
                        "scope": "link",
                    }
                ],
            },
        ]
        if self.ipv6_global:
            result[1]["addr_info"].append(
                {
                    "family": "inet6",
                    "local": "2001:db8::2",
                    "prefixlen": 64,
                    "scope": "global",
                }
            )
        return result

    def _sysctl_set(self, key, value):
        path = self.root / "proc/sys" / key
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(value)

    def __call__(self, args, **kwargs):
        args = list(args)
        self.commands.append((args, kwargs.get("input")))
        output = ""
        code = 0
        if args[0] == "systemctl":
            code, output = 3, "inactive\n"
        elif args[0] == "ip" and args[1:] in (
            ["-j", "-d", "link", "show"],
            ["-j", "link", "show"],
        ):
            output = self._all_links()
        elif args[:4] == ["ip", "-j", "-4", "address"]:
            output = self._all_addresses()
        elif args[:4] == ["ip", "-j", "-6", "address"]:
            output = self._all_ipv6()
        elif args[:5] == ["ip", "-j", "-4", "route", "show"]:
            output = json.dumps(self.routes)
        elif args[:5] == ["ip", "-j", "-6", "route", "show"]:
            output = "[]"
        elif args[:4] == ["ip", "-6", "address", "del"]:
            cidr, name = args[4], args[6]
            local, prefix = cidr.split("/", 1)
            device = next(item for item in self.ipv6 if item["ifname"] == name)
            device["addr_info"] = [
                info
                for info in device["addr_info"]
                if (info["local"], str(info["prefixlen"])) != (local, prefix)
            ]
        elif args[:4] == ["ip", "-6", "address", "add"]:
            cidr, name = args[4], args[6]
            local, prefix = cidr.split("/", 1)
            scope = args[args.index("scope") + 1]
            device = next((item for item in self.ipv6 if item["ifname"] == name), None)
            if device is None:
                device = {"ifname": name, "addr_info": []}
                self.ipv6.append(device)
            device["addr_info"].append(
                {
                    "family": "inet6",
                    "local": local,
                    "prefixlen": int(prefix),
                    "scope": scope,
                }
            )
        elif args[:3] == ["ip", "link", "add"]:
            parent = args[4]
            name = args[6]
            vlan_id = int(args[10])
            self._add_vlan()
            self.links[-1]["ifname"] = name
            self.links[-1]["link"] = parent
            self.links[-1]["linkinfo"]["info_data"]["id"] = vlan_id
        elif args[:3] == ["ip", "link", "set"]:
            name = args[4]
            link = next(item for item in self.links if item["ifname"] == name)
            if args[-1] == "up" and "UP" not in link["flags"]:
                link["flags"].append("UP")
            if args[-1] == "down":
                link["flags"] = [flag for flag in link["flags"] if flag != "UP"]
        elif args[:3] == ["ip", "link", "delete"]:
            self.links = [item for item in self.links if item["ifname"] != args[4]]
        elif args[:4] == ["ip", "-4", "address", "add"]:
            cidr, name = args[4], args[6]
            self.addresses.setdefault(name, []).append(cidr)
        elif args[:4] == ["ip", "-4", "address", "del"]:
            cidr, name = args[4], args[6]
            self.addresses[name].remove(cidr)
        elif args[:4] == ["ip", "-4", "route", "del"]:
            self._route_change(args, delete=True)
        elif args[:4] == ["ip", "-4", "route", "add"]:
            self._route_change(args, delete=False)
        elif args[:2] == ["sysctl", "-p"]:
            for line in Path(args[2]).read_text().splitlines():
                key, value = line.split("=", 1)
                self._sysctl_set(key.replace(".", "/"), value)
        elif args[:2] == ["sysctl", "-w"]:
            for setting in args[2:]:
                key, value = setting.split("=", 1)
                self._sysctl_set(key, value)
        elif args[:2] == ["resolvconf", "-a"]:
            record = self.root / "run/resolvconf/interface" / args[2]
            record.parent.mkdir(parents=True, exist_ok=True)
            record.write_text(kwargs.get("input", ""))
        elif args[:2] == ["resolvconf", "-d"]:
            (self.root / "run/resolvconf/interface" / args[2]).unlink(missing_ok=True)
        else:
            raise AssertionError(f"Unexpected mocked command: {args}")
        return subprocess.CompletedProcess(args, code, output, "")

    def _route_change(self, args, delete):
        dst = args[4]
        gateway = args[args.index("via") + 1] if "via" in args else None
        dev = args[args.index("dev") + 1] if "dev" in args else None
        metric = int(args[args.index("metric") + 1]) if "metric" in args else 0
        matches = [
            route
            for route in self.routes
            if route.get("dst", "default") == dst
            and route.get("gateway") == gateway
            and route.get("dev") == dev
            and route.get("metric", 0) == metric
        ]
        if delete:
            for route in matches:
                self.routes.remove(route)
        elif not matches:
            route = {"dst": dst, "dev": dev}
            if gateway:
                route["gateway"] = gateway
            if metric:
                route["metric"] = metric
            if "onlink" in args:
                route["flags"] = ["onlink"]
            self.routes.append(route)


class RouterNetworkTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "host"
        self.root.mkdir()
        self.directory = Path(self.temp.name) / "bundle"
        self.directory.mkdir()
        self.bundle = Bundle(self.root, self.directory)
        interfaces = (
            "auto lo\niface lo inet loopback\n"
            "auto wan0\niface wan0 inet static\n"
            "    address 192.0.2.2/24\n    gateway 192.0.2.1\n"
            "    dns-nameservers 1.1.1.1\n"
            "auto lan0\niface lan0 inet manual\n"
            "auto lan0.101\niface lan0.101 inet static\n"
            "    address 10.1.0.1/24\n    vlan-raw-device lan0\n"
        )
        (self.directory / "interfaces").write_text(interfaces)
        (self.directory / "sysctl.conf").write_text(
            "net.ipv4.ip_forward=1\n"
            "net.ipv4.conf.all.rp_filter=2\n"
            "net.ipv6.conf.all.disable_ipv6=1\n"
        )
        etc = self.root / "etc"
        (etc / "network").mkdir(parents=True)
        (etc / "network/interfaces").write_text(interfaces)
        (etc / "sysctl.d").mkdir()
        (etc / "sysctl.d/90-forcad-router.conf").write_bytes(
            (self.directory / "sysctl.conf").read_bytes()
        )
        etc.mkdir(exist_ok=True)
        (etc / "resolv.conf").symlink_to("../run/resolvconf/resolv.conf")
        (self.root / "run/resolvconf/interface").mkdir(parents=True)
        self._sysctl_tree()
        self.commands = FakeCommands(self.root)
        self.runner = patch.object(network.subprocess, "run", side_effect=self.commands)
        self.runner.start()
        self.addCleanup(self.runner.stop)
        self.which = patch.object(
            network.shutil, "which", return_value="/usr/sbin/resolvconf"
        )
        self.which.start()
        self.addCleanup(self.which.stop)
        self.manager = network.Network(self.bundle)
        self.guard = patch.object(network.Network, "_require_live_root")
        self.guard.start()
        self.addCleanup(self.guard.stop)

    def _sysctl_tree(self):
        proc = self.root / "proc/sys"
        values = {
            "net/ipv4/ip_forward": "0",
            "net/ipv4/conf/all/rp_filter": "1",
            "net/ipv4/conf/default/rp_filter": "1",
            "net/ipv4/conf/wan0/rp_filter": "1",
            "net/ipv4/conf/lan0/rp_filter": "1",
            "net/ipv6/conf/all/disable_ipv6": "0",
            "net/ipv6/conf/default/disable_ipv6": "0",
            "net/ipv6/conf/wan0/disable_ipv6": "0",
            "net/ipv6/conf/lan0/disable_ipv6": "0",
        }
        for name, value in values.items():
            path = proc / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(value)

    def _preflight(self):
        snapshot = self.manager.snapshot()
        self.manager.preflight(snapshot)
        return snapshot

    def test_preflight_rejects_dhcp_before_any_write(self):
        current = self.root / "etc/network/interfaces"
        current.write_text("auto wan0\niface wan0 inet dhcp\n")
        snapshot = self.manager.snapshot()
        with self.assertRaisesRegex(ValueError, "DHCP"):
            self.manager.preflight(snapshot)
        self.assertFalse(
            any(args[:3] == ["ip", "link", "add"] for args, _ in self.commands.commands)
        )

    def test_preflight_rejects_global_ipv6(self):
        self.commands.ipv6_global = True
        self.commands.ipv6[1]["addr_info"].append(
            {
                "family": "inet6",
                "local": "2001:db8::2",
                "prefixlen": 64,
                "scope": "global",
            }
        )
        snapshot = self.manager.snapshot()
        with self.assertRaisesRegex(ValueError, "Global IPv6 addresses"):
            self.manager.preflight(snapshot)

    def test_preflight_rejects_default_route_owned_by_another_device(self):
        self.commands.routes[0]["dev"] = "lan0"
        snapshot = self.manager.snapshot()
        with self.assertRaisesRegex(ValueError, "owned by another device"):
            self.manager.preflight(snapshot)
        mutations = [
            args
            for args, _ in self.commands.commands
            if args[0] in {"ip", "sysctl", "resolvconf"}
            and args[0:3]
            in (
                ["ip", "link", "add"],
                ["ip", "link", "set"],
                ["ip", "-4", "address"],
                ["ip", "-4", "route"],
                ["sysctl", "-p"],
                ["resolvconf", "-a"],
            )
        ]
        self.assertEqual(mutations, [])

    def test_snapshot_can_capture_journaled_scope_after_bundle_changes(self):
        saved_plan = self.manager.snapshot()["config"]
        self.bundle.config["routes"][0]["gateway"] = "192.0.2.254"
        snapshot = self.manager.snapshot(saved_plan)
        self.assertEqual(snapshot["config"], saved_plan)

    def test_matching_network_state_is_not_cycled(self):
        self.commands = FakeCommands(self.root, matched=True)
        self.runner.stop()
        self.runner = patch.object(network.subprocess, "run", side_effect=self.commands)
        self.runner.start()
        self.addCleanup(self.runner.stop)
        self.manager = network.Network(self.bundle)
        (self.root / "proc/sys/net/ipv4/ip_forward").write_text("1")
        (self.root / "proc/sys/net/ipv4/conf/all/rp_filter").write_text("2")
        (self.root / "proc/sys/net/ipv6/conf/all/disable_ipv6").write_text("1")
        (self.root / "run/resolvconf/interface/wan0.inet").write_text(
            "nameserver 1.1.1.1\n"
        )
        snapshot = self._preflight()
        self.manager.apply(snapshot)
        changed = [
            args
            for args, _ in self.commands.commands
            if args[:3] in (["ip", "link", "add"], ["ip", "link", "set"])
            or args[:4]
            in (
                ["ip", "-4", "address", "add"],
                ["ip", "-4", "route", "add"],
                ["ip", "-4", "route", "del"],
            )
            or args[:2] in (["sysctl", "-p"], ["resolvconf", "-a"])
        ]
        self.assertEqual(changed, [])

    def test_restore_returns_default_route_and_sysctls_to_snapshot(self):
        self.commands.routes[0]["gateway"] = "192.0.2.254"
        snapshot = self._preflight()
        self.manager.apply(snapshot)
        self.manager.restore(snapshot)
        self.assertEqual(
            self.manager._route_state(self.commands.routes[0]),
            snapshot["routes4"]["default"][0],
        )
        self.assertTrue(self.manager._route_state(self.commands.routes[0])["onlink"])
        writes = [
            args for args, _ in self.commands.commands if args[:2] == ["sysctl", "-w"]
        ]
        self.assertTrue(writes)
        self.assertEqual(writes[0][2], "net/ipv4/ip_forward=0")
        self.assertTrue(
            all(setting.startswith("net/ipv4/conf/") for setting in writes[1][2:])
        )
        self.assertNotIn("lan0.101", {item["ifname"] for item in self.commands.links})

    def test_restore_recreates_loopback_ipv6_and_route_onlink(self):
        snapshot = self._preflight()
        loopback = next(item for item in self.commands.ipv6 if item["ifname"] == "lo")
        loopback["addr_info"] = []
        self.manager.restore(snapshot)
        loopback = next(item for item in self.commands.ipv6 if item["ifname"] == "lo")
        self.assertEqual(loopback["addr_info"][0]["local"], "::1")
        self.assertIn(
            [
                "ip",
                "-6",
                "address",
                "add",
                "::1/128",
                "dev",
                "lo",
                "scope",
                "host",
            ],
            [args for args, _ in self.commands.commands],
        )

    def test_restore_preserves_new_ipv6_link_local_addresses(self):
        snapshot = self._preflight()
        self.commands.links.append({"ifname": "other0", "flags": ["BROADCAST", "UP"]})
        self.commands.ipv6.append(
            {
                "ifname": "other0",
                "addr_info": [
                    {
                        "family": "inet6",
                        "local": "fe80::99",
                        "prefixlen": 64,
                        "scope": "link",
                    }
                ],
            }
        )

        self.manager.restore(snapshot)

        address = next(
            info
            for item in self.commands.ipv6
            if item["ifname"] == "other0"
            for info in item["addr_info"]
        )
        self.assertEqual(address["local"], "fe80::99")
        self.assertFalse(
            any(
                args[:4] == ["ip", "-6", "address", "del"] and args[-1] == "other0"
                for args, _ in self.commands.commands
            )
        )

    def test_restore_allows_trusted_uninstall_recovery_transition(self):
        self.commands.routes[0]["gateway"] = "192.0.2.254"
        dns_path = self.root / "run/resolvconf/interface/wan0.inet"
        dns_path.write_text("nameserver 9.9.9.9\n")
        baseline = self._preflight()
        self.manager.apply(baseline)
        installed = self.manager.snapshot()

        self.manager.restore(baseline, applied=installed)
        self.assertEqual(self.commands.routes[0]["gateway"], "192.0.2.254")
        self.assertEqual(dns_path.read_text(), "nameserver 9.9.9.9\n")

        self.manager.restore(installed, allowed_current=baseline)
        self.assertEqual(self.commands.routes[0]["gateway"], "192.0.2.1")
        self.assertEqual(dns_path.read_text(), "nameserver 1.1.1.1\n")

    def test_restore_rejects_drift_before_mutating_host_state(self):
        snapshot = self._preflight()
        self.manager.apply(snapshot)
        applied = self.manager.snapshot()
        record = self.root / "run/resolvconf/interface/wan0.inet"
        record.write_text("nameserver 9.9.9.9\n")
        before = len(self.commands.commands)
        with self.assertRaisesRegex(ValueError, "WAN DNS record changed"):
            self.manager.restore(snapshot, applied=applied)
        mutations = [
            args
            for args, _ in self.commands.commands[before:]
            if args[:3] in (["ip", "link", "delete"], ["ip", "link", "set"])
            or args[:4]
            in (
                ["ip", "-4", "address", "del"],
                ["ip", "-4", "route", "add"],
                ["ip", "-4", "route", "del"],
                ["ip", "-6", "address", "add"],
                ["ip", "-6", "address", "del"],
            )
            or args[:2]
            in (["sysctl", "-w"], ["resolvconf", "-a"], ["resolvconf", "-d"])
        ]
        self.assertEqual(mutations, [])

    def test_restore_does_not_revert_unowned_sysctl_changes(self):
        snapshot = self._preflight()
        self.manager.apply(snapshot)
        applied = self.manager.snapshot()
        setting = self.root / "proc/sys/net/ipv4/conf/wan0/rp_filter"
        setting.write_text("9")
        self.manager.restore(snapshot, applied=applied)
        self.assertEqual(setting.read_text(), "9")
        writes = [
            args for args, _ in self.commands.commands if args[:2] == ["sysctl", "-w"]
        ]
        self.assertFalse(
            any("net/ipv4/conf/wan0/rp_filter=" in arg for cmd in writes for arg in cmd)
        )

    def test_restore_recreates_vlan_removed_during_failed_uninstall(self):
        snapshot = self._preflight()
        self.manager.apply(snapshot)
        before_uninstall = self.manager.snapshot()
        self.manager._run("ip", "link", "delete", "dev", "lan0.101")
        self.commands.routes.clear()
        self.assertNotIn("lan0.101", {item["ifname"] for item in self.commands.links})
        self.manager.restore(before_uninstall)
        self.assertIn("lan0.101", {item["ifname"] for item in self.commands.links})
        self.assertEqual(self.commands.addresses["lan0.101"], ["10.1.0.1/24"])
        self.assertEqual(
            self.manager._route_state(self.commands.routes[0]),
            before_uninstall["routes4"]["default"][0],
        )

    def test_check_restore_rejects_foreign_address_on_created_vlan(self):
        baseline = self._preflight()
        self.manager.apply(baseline)
        applied = self.manager.snapshot()
        self.commands.addresses["lan0.101"].append("10.99.0.1/24")
        current = self.manager.snapshot()
        before = len(self.commands.commands)
        with self.assertRaisesRegex(ValueError, "foreign IPv4 addresses"):
            self.manager.check_restore(baseline, applied, current)
        self.assertFalse(
            any(
                args[:4] == ["ip", "-4", "address", "del"]
                or args[:3] == ["ip", "link", "delete"]
                for args, _ in self.commands.commands[before:]
            )
        )


if __name__ == "__main__":
    unittest.main()

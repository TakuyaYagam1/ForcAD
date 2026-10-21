"""Docker cleanup plans are tested without touching the host firewall."""

import copy
import unittest
from unittest.mock import patch

import helpers  # noqa: F401 - add infrastructure modules to sys.path

from hostlib import docker_state


class DockerStateTests(unittest.TestCase):
    def snapshots(self):
        before = {
            "bridge": None,
            "tables": {
                f"{tool}:{table}": []
                for tool in ("iptables", "ip6tables")
                for table in ("filter", "nat")
            },
        }
        before["tables"]["iptables:filter"] = [
            ["-P", "FORWARD", "ACCEPT"],
            ["-A", "FORWARD", "-s", "192.0.2.1", "-j", "ACCEPT"],
        ]
        applied = copy.deepcopy(before)
        applied["tables"]["iptables:filter"][0][-1] = "DROP"
        applied["tables"]["iptables:filter"].extend(
            [
                ["-N", "DOCKER"],
                ["-A", "FORWARD", "-j", "DOCKER"],
                ["-A", "DOCKER", "-i", "docker0", "-j", "ACCEPT"],
            ]
        )
        return before, applied

    @patch("hostlib.docker_state.run")
    def test_restore_keeps_preexisting_and_unrelated_rules(self, run):
        before, applied = self.snapshots()
        current = copy.deepcopy(applied)
        current["tables"]["iptables:filter"].append(
            ["-A", "FORWARD", "-s", "192.0.2.2", "-j", "ACCEPT"]
        )
        docker_state.restore(before, applied, current)
        self.assertEqual(
            [call.args for call in run.call_args_list],
            [
                ("iptables", "-w", "-t", "filter", "-D", "FORWARD", "-j", "DOCKER"),
                (
                    "iptables",
                    "-w",
                    "-t",
                    "filter",
                    "-D",
                    "DOCKER",
                    "-i",
                    "docker0",
                    "-j",
                    "ACCEPT",
                ),
                ("iptables", "-w", "-t", "filter", "-X", "DOCKER"),
                ("iptables", "-w", "-t", "filter", "-P", "FORWARD", "ACCEPT"),
            ],
        )

    @patch("hostlib.docker_state.run")
    def test_foreign_rule_using_owned_chain_aborts_before_mutation(self, run):
        before, applied = self.snapshots()
        current = copy.deepcopy(applied)
        current["tables"]["iptables:filter"].append(
            ["-A", "DOCKER", "-s", "192.0.2.2", "-j", "ACCEPT"]
        )
        with self.assertRaisesRegex(ValueError, "Additional rules"):
            docker_state.restore(before, applied, current)
        run.assert_not_called()

    @patch("hostlib.docker_state.run")
    def test_repeated_restore_does_nothing(self, run):
        before, applied = self.snapshots()
        docker_state.restore(before, applied, before)
        run.assert_not_called()

    @patch("hostlib.docker_state.run")
    def test_rules_unrelated_to_docker_added_during_install_are_preserved(self, run):
        before, applied = self.snapshots()
        applied["tables"]["iptables:filter"].extend(
            [
                ["-N", "CUSTOM"],
                ["-A", "FORWARD", "-j", "CUSTOM"],
            ]
        )
        docker_state.restore(before, applied, applied)
        self.assertFalse(any("CUSTOM" in call.args for call in run.call_args_list))

    @patch("hostlib.docker_state.run", return_value="[]")
    def test_created_bridge_is_removed_but_existing_bridge_is_kept(self, run):
        before, applied = self.snapshots()
        applied["bridge"] = {
            "ifindex": 3,
            "ifname": "docker0",
            "address": "aa:bb:cc:dd:ee:ff",
            "linkinfo": {"info_kind": "bridge"},
        }
        docker_state.restore(before, applied, applied)
        self.assertEqual(run.call_args.args, ("ip", "link", "delete", "docker0"))
        run.reset_mock()
        docker_state.restore(applied, applied, applied)
        run.assert_not_called()

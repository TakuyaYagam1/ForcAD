"""Docker cleanup ownership checks use mocked CLI responses only."""

import copy
import json
import subprocess
import unittest
from unittest.mock import Mock, patch

import helpers  # noqa: F401 - add infrastructure modules to sys.path

from hostlib import docker_resources


def image_id(letter):
    return "sha256:" + letter * 64


def cache_id(letter):
    return letter * 25


def image(letter, tags=(), digests=(), parent=""):
    return {
        "id": image_id(letter),
        "tags": sorted(tags),
        "digests": sorted(digests),
        "parent": parent,
    }


def cache(letter, **changes):
    row = {
        "ID": cache_id(letter),
        "Parents": [],
        "CreatedAt": "2026-10-21T10:00:00Z",
        "Mutable": False,
        "Reclaimable": True,
        "Shared": False,
        "Size": "1024",
        "Description": "build cache",
        "UsageCount": 1,
        "LastUsedAt": "2026-10-21T10:00:00Z",
        "Type": "regular",
    }
    row.update(changes)
    return row


def snapshot(images=(), caches=(), root="/var/lib/docker"):
    return {
        "format": 1,
        "context": "default",
        "endpoint": "unix:///var/run/docker.sock",
        "docker_root_dir": root,
        "images": list(images),
        "cache": list(caches),
    }


class DockerResourceTests(unittest.TestCase):
    def install_snapshots(self):
        baseline_image = image("a", ["debian:stable"])
        added_image = image("b", ["forcad/api:latest"])
        baseline_cache = cache("c")
        added_cache = cache("d")
        before = snapshot([baseline_image], [baseline_cache])
        applied = snapshot([baseline_image, added_image], [baseline_cache, added_cache])
        return before, applied, baseline_image, added_image, baseline_cache, added_cache

    def exclusive_snapshots(self):
        added_image = image("b", ["forcad/api:latest"])
        added_cache = cache("d")
        return (
            snapshot(),
            snapshot([added_image], [added_cache]),
            added_image,
            added_cache,
        )

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_shared_cleanup_preserves_baseline_and_removes_added_resources(
        self, capture, run
    ):
        before, applied, old_image, _, old_cache, new_cache = self.install_snapshots()
        unknown_image = image("e", ["later:latest"])
        unknown_cache = cache("f")
        current = snapshot(
            [old_image, *applied["images"][1:], unknown_image],
            [old_cache, new_cache, unknown_cache],
        )
        after = snapshot([old_image, unknown_image], [old_cache, unknown_cache])
        capture.side_effect = [current, after]

        def command(*args):
            if args == ("docker", "container", "ls", "-aq", "--no-trunc"):
                return ""
            if args == ("docker", "image", "ls", "-aq", "--no-trunc"):
                return old_image["id"] + "\n"
            return ""

        run.side_effect = command
        docker_resources.cleanup(before, applied)

        run.assert_any_call("docker", "image", "rm", "--no-prune", "forcad/api:latest")
        run.assert_any_call(
            "docker",
            "buildx",
            "prune",
            "--builder",
            "default",
            "--all",
            "--force",
            "--filter",
            f"id~=^({new_cache['ID']})$",
        )
        removals = [
            call.args
            for call in run.call_args_list
            if call.args[1:3] == ("image", "rm")
        ]
        self.assertEqual(len(removals), 1)
        self.assertNotIn("--force", removals[0])
        prune_calls = [
            call.args
            for call in run.call_args_list
            if call.args[:3] == ("docker", "buildx", "prune")
        ]
        self.assertEqual(len(prune_calls), 1)
        self.assertIn("--filter", prune_calls[0])
        self.assertNotIn(old_image["id"], removals[0])
        self.assertNotIn(old_cache["ID"], prune_calls[0][-1])

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_tag_removal_does_not_retry_digest_removed_automatically(
        self, capture, run
    ):
        added_image = image(
            "b",
            ["registry.example/forcad/api:latest"],
            ["registry.example/forcad/api@sha256:" + "1" * 64],
        )
        before = snapshot()
        applied = snapshot([added_image])
        capture.side_effect = [copy.deepcopy(applied), snapshot()]
        image_exists = True

        def command(*args):
            nonlocal image_exists
            if args == ("docker", "container", "ls", "-aq", "--no-trunc"):
                return ""
            if args[:4] == ("docker", "image", "rm", "--no-prune"):
                self.assertEqual(args[-1], "registry.example/forcad/api:latest")
                image_exists = False
                return ""
            if args == ("docker", "image", "ls", "-aq", "--no-trunc"):
                return added_image["id"] + "\n" if image_exists else ""
            return ""

        run.side_effect = command

        docker_resources.cleanup(before, applied)

        removals = [
            call.args[-1]
            for call in run.call_args_list
            if call.args[:4] == ("docker", "image", "rm", "--no-prune")
        ]
        self.assertEqual(removals, ["registry.example/forcad/api:latest"])

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_same_image_reference_in_tags_and_digests_is_removed_once(
        self, capture, run
    ):
        shared_reference = "registry.example/forcad/api:latest"
        added_image = image("b", [shared_reference], [shared_reference])
        before = snapshot()
        applied = snapshot([added_image])
        capture.side_effect = [copy.deepcopy(applied), snapshot()]
        image_exists = True

        def command(*args):
            nonlocal image_exists
            if args == ("docker", "container", "ls", "-aq", "--no-trunc"):
                return ""
            if args[:4] == ("docker", "image", "rm", "--no-prune"):
                image_exists = False
                return ""
            if args == ("docker", "image", "ls", "-aq", "--no-trunc"):
                return added_image["id"] + "\n" if image_exists else ""
            return ""

        run.side_effect = command

        docker_resources.cleanup(before, applied)

        removals = [
            call.args[-1]
            for call in run.call_args_list
            if call.args[:4] == ("docker", "image", "rm", "--no-prune")
        ]
        self.assertEqual(removals, [shared_reference])

    @patch("hostlib.docker_resources.capture")
    def test_shared_reference_across_image_ids_fails_closed(self, capture):
        reference = "registry.example/forcad/api:latest"
        before = snapshot()
        applied = snapshot([image("b", [reference]), image("c", [reference])])

        with self.assertRaisesRegex(ValueError, "appears more than once"):
            docker_resources.cleanup(before, applied)

        capture.assert_not_called()

    @patch.dict("os.environ", {"DOCKER_HOST": ""})
    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources._engine_cache_rows")
    def test_capture_uses_raw_engine_cache_with_buildx_human_json(
        self, engine_rows, run
    ):
        buildx_row = {
            "CreatedAt": "2025-07-29 12:36:01 +0000 UTC",
            "Description": "pulled from docker.io/library/rust",
            "ID": "ic1gfidvev5nciupzz53alel4",
            "LastUsedAt": "About an hour ago",
            "Mutable": False,
            "Parents": None,
            "Reclaimable": True,
            "Shared": False,
            "Size": "829.9MB",
            "Type": "regular",
            "UsageCount": 1,
        }
        engine_rows.return_value = [
            {
                "ID": buildx_row["ID"],
                "Parents": [],
                "Type": "regular",
                "Description": "pulled from docker.io/library/rust",
                "InUse": False,
                "Shared": False,
                "Size": 829889526,
                "CreatedAt": "2025-07-29T12:36:01Z",
                "LastUsedAt": "2025-07-29T12:36:01Z",
                "UsageCount": 1,
            }
        ]
        run.side_effect = [
            "default\n",
            '[{"Name":"default","Endpoints":{"docker":{"Host":"unix:///var/run/docker.sock"}}}]',
            "/var/lib/docker\n",
            "",
            json.dumps(buildx_row) + "\n",
        ]

        result = docker_resources.capture()

        self.assertEqual(result["cache"][0]["ID"], buildx_row["ID"])
        self.assertEqual(result["cache"][0]["Size"], "829889526")
        self.assertEqual(result["cache"][0]["LastUsedAt"], "2025-07-29T12:36:01Z")
        self.assertEqual(result["cache"][0]["Parents"], [])
        run.assert_called_with(
            "docker", "buildx", "du", "--builder", "default", "--format=json"
        )
        engine_rows.assert_called_once_with("unix:///var/run/docker.sock")

    @patch("hostlib.docker_resources._UnixHTTPConnection")
    def test_engine_api_uses_local_socket_and_versioned_build_cache_endpoint(
        self, connection_class
    ):
        response = Mock()
        response.status = 200
        response.read.return_value = b'{"BuildCacheUsage":{"Items":[]}}'
        connection = Mock()
        connection.getresponse.return_value = response
        connection_class.return_value = connection

        result = docker_resources._engine_api_json("unix:///run/docker.sock", "1.53")

        self.assertEqual(result, {"BuildCacheUsage": {"Items": []}})
        connection_class.assert_called_once_with("/run/docker.sock")
        connection.request.assert_called_once_with(
            "GET", "/v1.53/system/df?type=build-cache&verbose=1"
        )
        connection.close.assert_called_once()

    def test_engine_cache_parser_accepts_empty_and_legacy_responses(self):
        self.assertEqual(
            docker_resources._parse_engine_cache(
                {"BuildCacheUsage": {"ActiveCount": 0, "TotalCount": 0}}
            ),
            [],
        )
        self.assertEqual(docker_resources._parse_engine_cache({"BuildCache": []}), [])

    def test_engine_cache_parser_normalizes_null_parents_and_raw_size(self):
        record = {
            "ID": cache_id("a"),
            "Parents": None,
            "Type": "regular",
            "Description": "build cache",
            "InUse": False,
            "Shared": False,
            "Size": 829889526,
            "CreatedAt": "2025-07-29T12:36:01Z",
            "LastUsedAt": None,
            "UsageCount": 1,
        }

        result = docker_resources._parse_engine_cache(
            {"BuildCacheUsage": {"Items": [record], "TotalCount": 1}}
        )

        self.assertEqual(result[0]["Parents"], [])
        self.assertEqual(result[0]["Size"], 829889526)
        self.assertIsNone(result[0]["LastUsedAt"])

    def test_cache_capture_rejects_mismatched_builder_ids(self):
        row = {
            "CreatedAt": "2025-07-29 12:36:01 +0000 UTC",
            "Description": "cache record",
            "ID": cache_id("a"),
            "LastUsedAt": "About an hour ago",
            "Mutable": False,
            "Parents": None,
            "Reclaimable": True,
            "Shared": False,
            "Size": "829.9MB",
            "Type": "regular",
            "UsageCount": 1,
        }

        with self.assertRaisesRegex(ValueError, "does not match"):
            docker_resources._cache_rows(json.dumps(row) + "\n", [])

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_exclusive_cleanup_rejects_unknown_images_and_cache_before_mutation(
        self, capture, run
    ):
        before, applied, _, _ = self.exclusive_snapshots()
        current = copy.deepcopy(applied)
        current["images"].append(image("e", ["later:latest"]))
        current["cache"].append(cache("f"))
        capture.return_value = current

        with self.assertRaisesRegex(ValueError, "Unrecorded Docker images"):
            docker_resources.cleanup(before, applied, exclusive=True)
        run.assert_not_called()

        current = copy.deepcopy(applied)
        current["cache"].append(cache("f"))
        capture.return_value = current
        with self.assertRaisesRegex(ValueError, "Unrecorded Buildx cache"):
            docker_resources.cleanup(before, applied, exclusive=True)
        run.assert_not_called()

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_exclusive_cleanup_rejects_retagged_image_before_mutation(
        self, capture, run
    ):
        before, applied, new_image, _ = self.exclusive_snapshots()
        current = copy.deepcopy(applied)
        current_image = next(
            row for row in current["images"] if row["id"] == new_image["id"]
        )
        current_image["tags"].append("later/api:latest")
        current_image["tags"].sort()
        capture.return_value = current

        with self.assertRaisesRegex(ValueError, "changed since installation"):
            docker_resources.cleanup(before, applied, exclusive=True)
        run.assert_not_called()

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_exclusive_cleanup_rejects_containers_before_mutation(self, capture, run):
        before, applied, *_ = self.exclusive_snapshots()
        capture.return_value = copy.deepcopy(applied)
        container_id = "1" * 64
        run.return_value = container_id + "\n"

        with self.assertRaisesRegex(ValueError, "containers prevent"):
            docker_resources.cleanup(before, applied, exclusive=True)
        self.assertEqual(
            run.call_args.args,
            ("docker", "container", "ls", "-aq", "--no-trunc"),
        )

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_exclusive_cleanup_rejects_volumes_and_custom_networks(self, capture, run):
        before, applied, *_ = self.exclusive_snapshots()
        capture.return_value = copy.deepcopy(applied)
        run.side_effect = ["", "forcad-data\n"]

        with self.assertRaisesRegex(ValueError, "volumes prevent"):
            docker_resources.cleanup(before, applied, exclusive=True)
        self.assertEqual(run.call_count, 2)

        run.reset_mock()
        run.side_effect = ["", "", "forcad-net\n"]
        with self.assertRaisesRegex(ValueError, "Custom Docker networks"):
            docker_resources.cleanup(before, applied, exclusive=True)
        self.assertEqual(run.call_count, 3)
        self.assertFalse(
            any(call.args[1:3] == ("image", "rm") for call in run.call_args_list)
        )

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_changed_owned_cache_is_preserved(self, capture, run):
        before, applied, *_ = self.install_snapshots()
        current = copy.deepcopy(applied)
        current["cache"][-1]["UsageCount"] += 1
        capture.return_value = current

        with self.assertRaisesRegex(ValueError, "Buildx cache changed"):
            docker_resources.cleanup(before, applied)
        run.assert_not_called()

    def test_cache_state_flags_may_change_when_record_identity_is_stable(self):
        before, applied, *_ = self.install_snapshots()
        applied["cache"][-1]["Reclaimable"] = False
        current = copy.deepcopy(applied)
        current["cache"][-1]["Shared"] = True
        current["cache"][-1]["Reclaimable"] = True

        self.assertEqual(
            docker_resources._cache_candidates(before, applied, current, False),
            [cache_id("d")],
        )

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_shared_cleanup_rejects_container_using_owned_image(self, capture, run):
        before, applied, _, owned_image, *_ = self.install_snapshots()
        capture.return_value = copy.deepcopy(applied)
        container_id = "1" * 64

        def command(*args):
            if args == ("docker", "container", "ls", "-aq", "--no-trunc"):
                return container_id + "\n"
            if args == ("docker", "container", "inspect", container_id):
                return json.dumps([{"Image": owned_image["id"]}])
            return ""

        run.side_effect = command

        with self.assertRaisesRegex(ValueError, "container uses an owned image"):
            docker_resources.cleanup(before, applied)
        self.assertFalse(
            any(call.args[1:3] == ("image", "rm") for call in run.call_args_list)
        )
        self.assertFalse(
            any(
                call.args[:3] == ("docker", "buildx", "prune")
                for call in run.call_args_list
            )
        )

    @patch("hostlib.docker_resources.run")
    @patch("hostlib.docker_resources.capture")
    def test_cleanup_retry_accepts_resources_partly_removed_by_prior_attempt(
        self, capture, run
    ):
        old_image = image("a", ["debian:stable"])
        first = image("b", ["forcad/one:latest"])
        second = image("c", ["forcad/two:latest"])
        old_cache, new_cache = cache("d"), cache("e")
        before = snapshot([old_image], [old_cache])
        applied = snapshot([old_image, first, second], [old_cache, new_cache])
        initial = copy.deepcopy(applied)
        retry = snapshot([old_image, second], [old_cache, new_cache])
        after = snapshot([old_image], [old_cache])
        capture.side_effect = [initial, retry, after]
        failed_once = False

        def command(*args):
            nonlocal failed_once
            if args == ("docker", "container", "ls", "-aq", "--no-trunc"):
                return ""
            if args[:4] == ("docker", "image", "rm", "--no-prune"):
                if args[-1] == "forcad/two:latest" and not failed_once:
                    failed_once = True
                    raise subprocess.CalledProcessError(1, args, stderr="in use")
                return ""
            if args == ("docker", "image", "ls", "-aq", "--no-trunc"):
                return old_image["id"] + "\n"
            return ""

        run.side_effect = command
        with self.assertRaises(subprocess.CalledProcessError):
            docker_resources.cleanup(before, applied)
        docker_resources.cleanup(before, applied)

        tag_removals = [
            call.args[-1]
            for call in run.call_args_list
            if call.args[:4] == ("docker", "image", "rm", "--no-prune")
        ]
        self.assertEqual(
            tag_removals,
            ["forcad/one:latest", "forcad/two:latest", "forcad/two:latest"],
        )
        self.assertEqual(capture.call_count, 3)

    @patch("hostlib.docker_resources.capture")
    @patch("hostlib.docker_resources.run")
    def test_unsupported_snapshot_fails_before_docker_access(self, run, capture):
        before = snapshot()
        applied = snapshot()
        applied["format"] = 2

        with self.assertRaisesRegex(ValueError, "snapshot version"):
            docker_resources.cleanup(before, applied)
        capture.assert_not_called()
        run.assert_not_called()

    @patch("hostlib.docker_resources.run")
    def test_capture_rejects_remote_context(self, run):
        run.side_effect = [
            "production\n",
            '[{"Name":"production","Endpoints":{"docker":{"Host":"ssh://host"}}}]',
        ]

        with self.assertRaisesRegex(ValueError, "local Unix socket"):
            docker_resources.capture()
        self.assertEqual(run.call_count, 2)


if __name__ == "__main__":
    unittest.main()

"""Capture and remove Docker resources created by a ForcAD installation."""

import argparse
import http.client
import json
import os
import re
import socket
import subprocess
from datetime import datetime
from pathlib import Path
from urllib.parse import urlsplit

from .common import regular

FORMAT = 1
IMAGE_ID = re.compile(r"sha256:[0-9a-f]{64}\Z")
CONTAINER_ID = re.compile(r"[0-9a-f]{64}\Z")
CACHE_ID = re.compile(r"[a-z0-9]{1,128}\Z")
SIZE = re.compile(r"[0-9]+\Z")
DEFAULT_NETWORKS = {"bridge", "host", "none"}
CACHE_FIELDS = {
    "ID",
    "Parents",
    "CreatedAt",
    "Mutable",
    "Reclaimable",
    "Shared",
    "Size",
    "Description",
    "UsageCount",
    "LastUsedAt",
    "Type",
}
BUILD_X_FIELDS = {
    "ID",
    "Parents",
    "CreatedAt",
    "Mutable",
    "Reclaimable",
    "Shared",
    "Size",
    "Description",
    "UsageCount",
    "LastUsedAt",
    "Type",
}
SNAPSHOT_FIELDS = {
    "format",
    "context",
    "endpoint",
    "docker_root_dir",
    "images",
    "cache",
}


def run(*args):
    return subprocess.run(
        args, check=True, capture_output=True, text=True, timeout=120
    ).stdout


def _json(text, description):
    def unique_object(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError(f"Duplicate JSON key in {description}: {key}")
            value[key] = item
        return value

    try:
        return json.loads(text, object_pairs_hook=unique_object)
    except (json.JSONDecodeError, TypeError) as error:
        raise ValueError(f"Invalid JSON from {description}") from error


def _lines(text, description):
    values = [line for line in text.splitlines() if line]
    if any(any(ord(char) < 32 for char in value) for value in values):
        raise ValueError(f"Invalid {description} output")
    return values


def _local_endpoint(endpoint):
    if not isinstance(endpoint, str) or any(ord(char) < 32 for char in endpoint):
        raise ValueError("Invalid Docker context endpoint")
    parsed = urlsplit(endpoint)
    if (
        parsed.scheme != "unix"
        or parsed.netloc
        or not parsed.path.startswith("/")
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError("Docker context must use a local Unix socket")
    return endpoint


def _context():
    name = run("docker", "context", "show").strip()
    if not name or any(ord(char) < 32 for char in name):
        raise ValueError("Invalid Docker context name")
    inspected = _json(
        run("docker", "context", "inspect", "--format", "json", name),
        "Docker context",
    )
    if (
        not isinstance(inspected, list)
        or len(inspected) != 1
        or not isinstance(inspected[0], dict)
        or inspected[0].get("Name") != name
    ):
        raise ValueError("Unsupported Docker context data")
    endpoints = inspected[0].get("Endpoints")
    docker = endpoints.get("docker") if isinstance(endpoints, dict) else None
    if not isinstance(docker, dict):
        raise TypeError("Docker context has no Docker endpoint")
    endpoint = _local_endpoint(docker.get("Host"))

    override = os.environ.get("DOCKER_HOST")
    if override and _local_endpoint(override) != endpoint:
        raise ValueError("DOCKER_HOST does not match the local Docker context")
    return name, endpoint


def _image_id(value, description="image ID"):
    if not isinstance(value, str) or not IMAGE_ID.fullmatch(value):
        raise ValueError(f"Invalid {description}")
    return value


def _reference(value):
    if (
        not isinstance(value, str)
        or not value
        or value.startswith("-")
        or any(ord(char) < 32 for char in value)
    ):
        raise ValueError("Invalid Docker image reference")
    return value


def _inspect_images(ids):
    if not ids:
        return []
    rows = _json(run("docker", "image", "inspect", *ids), "Docker image inspect")
    if not isinstance(rows, list) or len(rows) != len(ids):
        raise ValueError("Unsupported Docker image inspect data")
    result = []
    for row in rows:
        if not isinstance(row, dict):
            raise TypeError("Unsupported Docker image inspect data")
        image_id = _image_id(row.get("Id"))
        tags = row.get("RepoTags")
        digests = row.get("RepoDigests")
        parent = row.get("Parent", "")
        if tags is None:
            tags = []
        if digests is None:
            digests = []
        if not isinstance(tags, list) or not isinstance(digests, list):
            raise TypeError("Unsupported Docker image references")
        if parent is None:
            parent = ""
        if parent:
            _image_id(parent, "Docker parent image ID")
        image = {
            "id": image_id,
            "tags": sorted({_reference(value) for value in tags}),
            "digests": sorted({_reference(value) for value in digests}),
            "parent": parent,
        }
        if len(image["tags"]) != len(tags) or len(image["digests"]) != len(digests):
            raise ValueError("Duplicate Docker image reference")
        result.append(image)
    if len({image["id"] for image in result}) != len(result):
        raise ValueError("Duplicate Docker image ID")
    return sorted(result, key=lambda image: image["id"])


def _buildx_cache_rows(text):
    rows = {}
    for line in text.splitlines():
        if not line:
            continue
        row = _json(line, "Buildx cache usage")
        if not isinstance(row, dict) or frozenset(row) not in {
            frozenset(BUILD_X_FIELDS),
            frozenset(BUILD_X_FIELDS - {"LastUsedAt"}),
        }:
            raise ValueError("Unsupported Buildx cache data")
        if not isinstance(row["ID"], str) or not CACHE_ID.fullmatch(row["ID"]):
            raise ValueError("Invalid Buildx cache ID")
        parents = row["Parents"]
        if parents is not None and not isinstance(parents, list):
            raise TypeError("Unsupported Buildx cache parents")
        for parent in parents or []:
            if not isinstance(parent, str) or not CACHE_ID.fullmatch(parent):
                raise ValueError("Invalid Buildx cache parent ID")
        if not all(
            isinstance(row[key], str) for key in ("CreatedAt", "Description", "Type")
        ):
            raise ValueError("Unsupported Buildx cache text field")
        if not isinstance(row["Size"], str) or not row["Size"]:
            raise ValueError("Invalid Buildx cache size")
        if "LastUsedAt" in row and not isinstance(row["LastUsedAt"], str):
            raise ValueError("Unsupported Buildx cache last-used value")
        if not all(
            type(row[key]) is bool for key in ("Mutable", "Reclaimable", "Shared")
        ):
            raise ValueError("Unsupported Buildx cache flags")
        if not all(type(row[key]) is int and row[key] >= 0 for key in ("UsageCount",)):
            raise ValueError("Unsupported Buildx cache counters")
        if len(set(parents or [])) != len(parents or []):
            raise ValueError("Duplicate Buildx cache parent ID")
        if row["ID"] in rows:
            raise ValueError("Duplicate Buildx cache ID")
        rows[row["ID"]] = row
    return rows


def _cache_rows(text, engine_rows):
    buildx = _buildx_cache_rows(text)
    engine = {row["ID"]: row for row in engine_rows}
    if len(engine) != len(engine_rows) or set(buildx) != set(engine):
        raise ValueError("Buildx cache does not match local Docker daemon cache")
    rows = []
    for cache_id, row in engine.items():
        flags = buildx[cache_id]
        if flags["Shared"] != row["Shared"] or flags["Reclaimable"] != (
            not row["InUse"]
        ):
            raise ValueError(
                f"Buildx cache state does not match Docker daemon: {cache_id}"
            )
        if (
            sorted(flags["Parents"] or []) != row["Parents"]
            or flags["Description"] != row["Description"]
            or flags["Type"] != row["Type"]
            or flags["UsageCount"] != row["UsageCount"]
        ):
            raise ValueError(
                f"Buildx cache record does not match Docker daemon: {cache_id}"
            )
        rows.append(
            {
                "ID": cache_id,
                "Parents": row["Parents"],
                "CreatedAt": row["CreatedAt"],
                "Mutable": flags["Mutable"],
                "Reclaimable": not row["InUse"],
                "Shared": row["Shared"],
                "Size": str(row["Size"]),
                "Description": row["Description"],
                "UsageCount": row["UsageCount"],
                "LastUsedAt": row["LastUsedAt"],
                "Type": row["Type"],
            }
        )
    return sorted(rows, key=lambda row: row["ID"])


def _timestamp(value, description):
    if not isinstance(value, str):
        raise TypeError(f"Invalid {description} timestamp")
    normalized = value.replace("Z", "+00:00")
    normalized = re.sub(r"(\.\d{6})\d+(?=[+-]\d\d:\d\d$)", r"\1", normalized)
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError as error:
        raise ValueError(f"Invalid {description} timestamp") from error
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ValueError(f"Invalid {description} timestamp")
    return parsed


def _parse_engine_cache(payload):
    if not isinstance(payload, dict):
        raise TypeError("Unsupported Docker system disk usage data")
    if "BuildCacheUsage" in payload:
        usage = payload["BuildCacheUsage"]
    elif "BuildCache" in payload:
        usage = payload["BuildCache"]
    else:
        raise ValueError("Docker system disk usage has no build cache section")
    if isinstance(usage, dict):
        records = usage.get("Items", [])
        if records is None:
            records = []
        total_count = usage.get("TotalCount")
        if total_count is not None and (
            type(total_count) is not int
            or total_count < 0
            or total_count != len(records)
        ):
            raise ValueError("Incomplete Docker system disk usage data")
    else:
        records = usage
        if records is None:
            records = []
    if not isinstance(records, list):
        raise TypeError("Docker system disk usage has no build cache records")
    rows = []
    for record in records:
        if not isinstance(record, dict):
            raise TypeError("Unsupported Docker build cache record")
        parent_keys = {"Parents", " Parents"} & set(record)
        if len(parent_keys) > 1:
            raise ValueError("Duplicate Docker build cache parents field")
        parents_key = next(iter(parent_keys), "Parents")
        parents = record.get(parents_key)
        if parents is None:
            parents = []
        required = {
            "ID",
            "Type",
            "Description",
            "InUse",
            "Shared",
            "Size",
            "CreatedAt",
            "UsageCount",
        }
        if not required.issubset(record) or not isinstance(parents, list):
            raise ValueError("Unsupported Docker build cache record")
        cache_id = record["ID"]
        if not isinstance(cache_id, str) or not CACHE_ID.fullmatch(cache_id):
            raise ValueError("Invalid Docker build cache ID")
        if any(
            not isinstance(parent, str) or not CACHE_ID.fullmatch(parent)
            for parent in parents
        ):
            raise ValueError("Invalid Docker build cache parent ID")
        if len(set(parents)) != len(parents):
            raise ValueError("Duplicate Docker build cache parent ID")
        if not all(
            isinstance(record[key], str) for key in ("Type", "Description", "CreatedAt")
        ):
            raise ValueError("Unsupported Docker build cache text field")
        _timestamp(record["CreatedAt"], "Docker build cache created-at")
        if type(record["Size"]) is not int or record["Size"] < 0:
            raise ValueError("Invalid Docker build cache size")
        if type(record["InUse"]) is not bool or type(record["Shared"]) is not bool:
            raise ValueError("Unsupported Docker build cache state")
        if type(record["UsageCount"]) is not int or record["UsageCount"] < 0:
            raise ValueError("Unsupported Docker build cache usage count")
        last_used = record.get("LastUsedAt")
        if last_used is not None and not isinstance(last_used, str):
            raise ValueError("Unsupported Docker build cache last-used value")
        if last_used is not None:
            _timestamp(last_used, "Docker build cache last-used")
        rows.append(
            {
                "ID": cache_id,
                "Parents": sorted(parents),
                "CreatedAt": record["CreatedAt"],
                "Description": record["Description"],
                "InUse": record["InUse"],
                "LastUsedAt": last_used,
                "Shared": record["Shared"],
                "Size": record["Size"],
                "Type": record["Type"],
                "UsageCount": record["UsageCount"],
            }
        )
    if len({row["ID"] for row in rows}) != len(rows):
        raise ValueError("Duplicate Buildx cache ID")
    return sorted(rows, key=lambda row: row["ID"])


class _UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, socket_path):
        super().__init__("localhost", timeout=120)
        self.socket_path = socket_path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.socket_path)


def _engine_api_json(endpoint, api_version):
    socket_path = urlsplit(endpoint).path
    connection = _UnixHTTPConnection(socket_path)
    try:
        request_path = f"/v{api_version}/system/df?type=build-cache&verbose=1"
        connection.request("GET", request_path)
        response = connection.getresponse()
        body = response.read()
        if response.status != 200:
            raise ValueError(
                f"Docker system disk usage API failed: HTTP {response.status}"
            )
        try:
            text = body.decode("utf-8")
        except UnicodeDecodeError as error:
            raise ValueError("Invalid Docker system disk usage response") from error
        return _json(text, "Docker system disk usage")
    except (OSError, http.client.HTTPException) as error:
        raise ValueError(
            "Cannot query the local Docker system disk usage API"
        ) from error
    finally:
        connection.close()


def _engine_cache_rows(endpoint):
    api_version = run("docker", "version", "--format", "{{.Server.APIVersion}}").strip()
    if (
        not re.fullmatch(r"1\.[0-9]+", api_version)
        or int(api_version.split(".")[1]) < 42
    ):
        raise ValueError("Unsupported Docker Engine API version for build cache data")
    return _parse_engine_cache(_engine_api_json(endpoint, api_version))


def capture():
    """Return a validated snapshot of the local Docker daemon and default builder."""
    context, endpoint = _context()
    root_dir = run("docker", "info", "--format", "{{.DockerRootDir}}").strip()
    if not root_dir.startswith("/") or any(ord(char) < 32 for char in root_dir):
        raise ValueError("Invalid Docker data directory")

    ids = sorted(
        {
            _image_id(value, "Docker image list ID")
            for value in _lines(
                run("docker", "image", "ls", "-aq", "--no-trunc"),
                "Docker image list",
            )
        }
    )
    images = _inspect_images(ids)
    buildx_cache = run(
        "docker", "buildx", "du", "--builder", "default", "--format=json"
    )
    cache = _cache_rows(buildx_cache, _engine_cache_rows(endpoint))
    snapshot = {
        "format": FORMAT,
        "context": context,
        "endpoint": endpoint,
        "docker_root_dir": root_dir,
        "images": images,
        "cache": cache,
    }
    _validate_snapshot(snapshot)
    return snapshot


def _validate_snapshot(snapshot):
    if not isinstance(snapshot, dict) or set(snapshot) != SNAPSHOT_FIELDS:
        raise ValueError("Unsupported Docker resource snapshot")
    if type(snapshot["format"]) is not int or snapshot["format"] != FORMAT:
        raise ValueError("Unsupported Docker resource snapshot version")
    if not isinstance(snapshot["context"], str) or not snapshot["context"]:
        raise ValueError("Invalid Docker context in snapshot")
    _local_endpoint(snapshot["endpoint"])
    root_dir = snapshot["docker_root_dir"]
    if (
        not isinstance(root_dir, str)
        or not root_dir.startswith("/")
        or any(ord(char) < 32 for char in root_dir)
    ):
        raise ValueError("Invalid Docker data directory in snapshot")
    if not isinstance(snapshot["images"], list) or not isinstance(
        snapshot["cache"], list
    ):
        raise TypeError("Invalid Docker resource lists in snapshot")

    images = {}
    references = {}
    for image in snapshot["images"]:
        if not isinstance(image, dict) or set(image) != {
            "id",
            "tags",
            "digests",
            "parent",
        }:
            raise ValueError("Unsupported Docker image snapshot")
        image_id = _image_id(image["id"])
        parent = image["parent"]
        if not isinstance(parent, str):
            raise TypeError("Invalid Docker parent image ID")
        if parent:
            _image_id(parent, "Docker parent image ID")
        image_references = set()
        for key in ("tags", "digests"):
            values = image[key]
            if not isinstance(values, list):
                raise TypeError("Invalid Docker image references")
            if len(set(values)) != len(values):
                raise ValueError("Duplicate Docker image reference")
            for value in values:
                _reference(value)
                owner = references.get(value)
                if owner is not None and owner != image_id:
                    raise ValueError("Docker image reference appears more than once")
                image_references.add(value)
        for value in image_references:
            references[value] = image_id
        if image_id in images:
            raise ValueError("Duplicate Docker image ID in snapshot")
        images[image_id] = image

    cache_ids = set()
    for row in snapshot["cache"]:
        if not isinstance(row, dict) or set(row) != CACHE_FIELDS:
            raise ValueError("Unsupported Buildx cache snapshot")
        cache_id = row["ID"]
        if not isinstance(cache_id, str) or not CACHE_ID.fullmatch(cache_id):
            raise ValueError("Invalid Buildx cache ID")
        if cache_id in cache_ids:
            raise ValueError("Duplicate Buildx cache ID in snapshot")
        cache_ids.add(cache_id)
        if (
            not isinstance(row["Parents"], list)
            or any(
                not isinstance(parent, str) or not CACHE_ID.fullmatch(parent)
                for parent in row["Parents"]
            )
            or not all(
                isinstance(row[key], str)
                for key in ("CreatedAt", "Description", "Type")
            )
            or not isinstance(row["Size"], str)
            or not SIZE.fullmatch(row["Size"])
            or (
                row["LastUsedAt"] is not None and not isinstance(row["LastUsedAt"], str)
            )
            or not all(
                type(row[key]) is bool for key in ("Mutable", "Reclaimable", "Shared")
            )
            or type(row["UsageCount"]) is not int
            or row["UsageCount"] < 0
        ):
            raise ValueError("Invalid Buildx cache snapshot")
        _timestamp(row["CreatedAt"], "Buildx cache created-at")
        if row["LastUsedAt"] is not None:
            _timestamp(row["LastUsedAt"], "Buildx cache last-used")
    return snapshot


def _snapshot_map(snapshot, key, identifier):
    return {row[identifier]: row for row in snapshot[key]}


def _check_history(before, applied, current):
    for key in ("context", "endpoint", "docker_root_dir"):
        if before[key] != applied[key] or applied[key] != current[key]:
            raise ValueError(
                f"Docker {key.replace('_', ' ')} changed since installation"
            )


def _image_candidates(before, applied, current, exclusive):
    old = _snapshot_map(before, "images", "id")
    installed = _snapshot_map(applied, "images", "id")
    live = _snapshot_map(current, "images", "id")
    candidates = set(installed) - set(old)
    if exclusive:
        unknown = set(live) - set(installed)
        if unknown:
            raise ValueError("Unrecorded Docker images prevent exclusive cleanup")
    for image_id in candidates & set(live):
        expected, actual = installed[image_id], live[image_id]
        if (
            actual["parent"] != expected["parent"]
            or not set(actual["tags"]).issubset(expected["tags"])
            or not set(actual["digests"]).issubset(expected["digests"])
        ):
            raise ValueError(f"Docker image changed since installation: {image_id}")
    if exclusive:
        for image_id, actual in live.items():
            expected = installed[image_id]
            if (
                actual["parent"] != expected["parent"]
                or not set(actual["tags"]).issubset(expected["tags"])
                or not set(actual["digests"]).issubset(expected["digests"])
            ):
                raise ValueError(f"Docker image changed since installation: {image_id}")

    present = candidates & set(live)
    for image in live.values():
        if image["parent"] in present and image["id"] not in present:
            raise ValueError(f"Docker image depends on an owned image: {image['id']}")
    children = {image_id: [] for image_id in present}
    for image_id in present:
        parent = live[image_id]["parent"]
        if parent in present:
            children[parent].append(image_id)
    ordered = []
    visited = set()

    def visit(image_id):
        if image_id in visited:
            return
        visited.add(image_id)
        for child in sorted(children[image_id]):
            visit(child)
        ordered.append(image_id)

    for image_id in sorted(present):
        visit(image_id)
    return ordered, live, installed


def _cache_candidates(before, applied, current, exclusive):
    old = _snapshot_map(before, "cache", "ID")
    installed = _snapshot_map(applied, "cache", "ID")
    live = _snapshot_map(current, "cache", "ID")
    candidates = set(installed) - set(old)
    if exclusive and set(live) - set(installed):
        raise ValueError("Unrecorded Buildx cache prevents exclusive cleanup")
    for cache_id in candidates & set(live):
        if _cache_fingerprint(live[cache_id]) != _cache_fingerprint(
            installed[cache_id]
        ):
            raise ValueError(f"Buildx cache changed since installation: {cache_id}")
        if not live[cache_id]["Reclaimable"]:
            raise ValueError(f"Buildx cache is not reclaimable: {cache_id}")
    if exclusive:
        for cache_id, row in live.items():
            if _cache_fingerprint(row) != _cache_fingerprint(installed[cache_id]):
                raise ValueError(f"Buildx cache changed since installation: {cache_id}")
    return sorted(candidates & set(live))


def _cache_fingerprint(row):
    return tuple(
        row[key]
        for key in (
            "ID",
            "Parents",
            "CreatedAt",
            "Mutable",
            "Size",
            "Description",
            "UsageCount",
            "LastUsedAt",
            "Type",
        )
    )


def _container_images(candidate_ids):
    ids = sorted(
        {
            value
            for value in _lines(
                run("docker", "container", "ls", "-aq", "--no-trunc"),
                "Docker container list",
            )
        }
    )
    if any(not CONTAINER_ID.fullmatch(value) for value in ids):
        raise ValueError("Invalid Docker container ID")
    if not ids or not candidate_ids:
        return ids
    rows = _json(
        run("docker", "container", "inspect", *ids), "Docker container inspect"
    )
    if not isinstance(rows, list) or len(rows) != len(ids):
        raise ValueError("Unsupported Docker container inspect data")
    used = set()
    for row in rows:
        image_id = row.get("Image") if isinstance(row, dict) else None
        used.add(_image_id(image_id, "Docker container image ID"))
    if used & candidate_ids:
        raise ValueError("A Docker container uses an owned image")
    return ids


def _exclusive_resources():
    containers = _lines(
        run("docker", "container", "ls", "-aq", "--no-trunc"),
        "Docker container list",
    )
    if containers:
        raise ValueError("Docker containers prevent exclusive cleanup")
    volumes = _lines(run("docker", "volume", "ls", "-q"), "Docker volume list")
    if volumes:
        raise ValueError("Docker volumes prevent exclusive cleanup")
    networks = _lines(
        run("docker", "network", "ls", "--format", "{{.Name}}"),
        "Docker network list",
    )
    if set(networks) - DEFAULT_NETWORKS:
        raise ValueError("Custom Docker networks prevent exclusive cleanup")


def _remove_images(ordered, current, installed):
    for image_id in ordered:
        image = current[image_id]
        references = sorted(set(image["tags"] + image["digests"]))
        live_references = set(references)
        for reference in references:
            if reference not in live_references:
                continue
            run("docker", "image", "rm", "--no-prune", reference)
            remaining = {
                _image_id(value, "Docker image list ID")
                for value in _lines(
                    run("docker", "image", "ls", "-aq", "--no-trunc"),
                    "Docker image list",
                )
            }
            if image_id not in remaining:
                break
            row = _inspect_images([image_id])[0]
            expected = installed[image_id]
            if (
                row["parent"] != expected["parent"]
                or not set(row["tags"]).issubset(expected["tags"])
                or not set(row["digests"]).issubset(expected["digests"])
            ):
                raise ValueError(f"Docker image changed during cleanup: {image_id}")
            live_references = set(row["tags"] + row["digests"])
            if reference in live_references:
                raise ValueError(
                    f"Docker image reference remains after cleanup: {reference}"
                )

    remaining = {
        _image_id(value, "Docker image list ID")
        for value in _lines(
            run("docker", "image", "ls", "-aq", "--no-trunc"),
            "Docker image list",
        )
    }
    for image_id in ordered:
        if image_id not in remaining:
            continue
        row = _inspect_images([image_id])[0]
        expected = installed[image_id]
        if row["parent"] != expected["parent"] or row["tags"] or row["digests"]:
            raise ValueError(f"Docker image changed during cleanup: {image_id}")
        run("docker", "image", "rm", "--no-prune", image_id)


def _read_snapshot(path):
    try:
        snapshot = _json(regular(path).read_text(), str(path))
    except OSError as error:
        raise ValueError(f"Cannot read Docker resource snapshot: {path}") from error
    return _validate_snapshot(snapshot)


def cleanup(before, applied, *, exclusive=False):
    """Remove recorded resources after validating ownership and current use."""
    before = _validate_snapshot(before)
    applied = _validate_snapshot(applied)
    current = _validate_snapshot(capture())
    _check_history(before, applied, current)
    if exclusive:
        if before["images"] or before["cache"]:
            raise ValueError("Pre-existing Docker resources prevent exclusive cleanup")
        if applied["docker_root_dir"] != "/var/lib/docker":
            raise ValueError("Docker data directory is not /var/lib/docker")

    images, live_images, installed_images = _image_candidates(
        before, applied, current, exclusive
    )
    cache_ids = _cache_candidates(before, applied, current, exclusive)
    if exclusive:
        _exclusive_resources()
    else:
        _container_images(set(images))

    if images:
        _remove_images(images, live_images, installed_images)
    if cache_ids:
        pattern = "^(" + "|".join(cache_ids) + ")$"
        run(
            "docker",
            "buildx",
            "prune",
            "--builder",
            "default",
            "--all",
            "--force",
            "--filter",
            f"id~={pattern}",
        )

    remaining = _validate_snapshot(capture())
    remaining_images = set(_snapshot_map(remaining, "images", "id"))
    remaining_cache = set(_snapshot_map(remaining, "cache", "ID"))
    owned_images = set(_snapshot_map(applied, "images", "id")) - set(
        _snapshot_map(before, "images", "id")
    )
    owned_cache = set(_snapshot_map(applied, "cache", "ID")) - set(
        _snapshot_map(before, "cache", "ID")
    )
    if owned_images & remaining_images:
        raise ValueError("Owned Docker images remain after cleanup")
    if owned_cache & remaining_cache:
        raise ValueError("Owned Buildx cache remains after cleanup")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("capture", "cleanup"))
    parser.add_argument("before", nargs="?", type=Path)
    parser.add_argument("applied", nargs="?", type=Path)
    parser.add_argument("--exclusive", action="store_true")
    args = parser.parse_args(argv)
    try:
        if args.action == "capture":
            if args.before or args.applied or args.exclusive:
                parser.error("capture does not take snapshot paths or --exclusive")
            print(json.dumps(capture(), sort_keys=True))
            return
        if args.before is None or args.applied is None:
            parser.error("cleanup requires the before and applied snapshots")
        cleanup(
            _read_snapshot(args.before),
            _read_snapshot(args.applied),
            exclusive=args.exclusive,
        )
    except (
        OSError,
        ValueError,
        TypeError,
        KeyError,
        subprocess.SubprocessError,
    ) as error:
        parser.exit(1, f"FAIL {error}\n")


if __name__ == "__main__":
    main()

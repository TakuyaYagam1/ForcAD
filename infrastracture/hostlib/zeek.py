"""Install or reuse a supported Zeek runtime on a Debian router."""

import hashlib
import re
import stat

from .common import regular

KEY_SHA256 = "b67ae73b8543af4195c930b52a07c208611253fdc519873adcf1190ffcfc1377"
KEY_PATH = "/etc/apt/keyrings/forcad-zeek.asc"
SOURCE_PATH = "/etc/apt/sources.list.d/forcad-zeek.list"
SUPPORTED_DISTRIBUTIONS = {"12", "13"}
SUPPORTED_ARCHITECTURES = {"amd64", "arm64"}
VERSION_PATTERN = re.compile(
    r"(?i)\b(?:zeek\s+)?(?:version\s+)?v?(\d+(?:\.\d+){1,2}(?:[-+][0-9a-z.+-]+)?)\b"
)


def _text(value):
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    return str(value or "")


def _run(deps, *args, text=True):
    result = deps.run(
        *args,
        capture_output=True,
        text=text,
        check=False,
        timeout=30,
    )
    if result.returncode:
        detail = _text(result.stderr).strip() or _text(result.stdout).strip()
        command = " ".join(map(str, args))
        if detail:
            raise ValueError(f"Command failed: {command}: {detail}")
        raise ValueError(f"Command failed: {command}")
    return result


def _version(deps, binary):
    result = _run(deps, str(binary), "--version")
    output = _text(result.stdout).strip() or _text(result.stderr).strip()
    match = VERSION_PATTERN.search(output)
    if not match:
        raise ValueError(f"Could not read the Zeek version from: {output}")
    version = match.group(1)
    if "-" in version.split("+", 1)[0]:
        raise ValueError(f"A stable Zeek 9 or newer is required; found {version}")
    major = int(version.split(".", 1)[0])
    if major < 9:
        raise ValueError(f"Zeek 9 or newer is required; found {version}")
    return version


def _validate_runtime_path(deps, binary):
    binary = regular(binary)
    zeek_root = deps.path("/opt/zeek")
    paths = (
        (deps.path("/opt"), stat.S_ISDIR),
        (zeek_root, stat.S_ISDIR),
        (zeek_root / "bin", stat.S_ISDIR),
        (binary, stat.S_ISREG),
    )
    writable = stat.S_IWGRP | stat.S_IWOTH
    for path, is_expected_type in paths:
        info = path.stat()
        if not is_expected_type(info.st_mode):
            raise ValueError(f"Unsafe Zeek runtime path type: {path}")
        if info.st_uid != 0:
            raise ValueError(f"Zeek runtime path is not root-owned: {path}")
        if info.st_mode & writable:
            raise ValueError(f"Zeek runtime path is group/world-writable: {path}")
    return binary


def _base_packages(packages):
    return {name.split(":", 1)[0] for name in packages}


def _finish_partial_install(deps):
    package = "zeek-9.0-core"
    recorded = deps.data.get("packages", [])
    if not isinstance(recorded, list):
        raise TypeError("Invalid package ownership state")
    if package not in _base_packages(recorded):
        return False

    present = _base_packages(deps.present())
    installed = _base_packages(deps.installed())
    if package not in present or package in installed:
        return False

    _install_zeek_package(deps)
    if package not in _base_packages(deps.installed()):
        raise ValueError("Could not finish the Zeek package installation")
    return True


def _group_gid(deps):
    result = deps.run(
        "getent",
        "group",
        "zeek",
        capture_output=True,
        text=True,
        check=False,
        timeout=30,
    )
    if result.returncode == 2:
        return None
    if result.returncode:
        detail = _text(result.stderr).strip() or _text(result.stdout).strip()
        raise ValueError(f"Could not inspect the zeek group: {detail}")
    fields = _text(result.stdout).strip().split(":")
    if len(fields) < 4 or fields[0] != "zeek":
        raise ValueError("Could not parse the zeek group entry")
    try:
        return int(fields[2])
    except ValueError as error:
        raise ValueError("The zeek group has an invalid GID") from error


def _record_group(deps, before_gid, after_gid, required):
    accounts = deps.data.setdefault("accounts", {})
    if not isinstance(accounts, dict):
        raise TypeError("Invalid account ownership state")
    if "zeek_group" in accounts:
        try:
            recorded_gid = int(accounts["zeek_group"])
        except (TypeError, ValueError) as error:
            raise ValueError("Invalid recorded zeek group GID") from error
        if after_gid != recorded_gid:
            raise ValueError("The recorded zeek group changed during installation")
        return
    if before_gid is not None:
        if after_gid != before_gid:
            raise ValueError("The pre-existing zeek group changed during installation")
        return
    if after_gid is None:
        if required:
            raise ValueError("The Zeek package did not create the zeek group")
        return
    accounts["zeek_group"] = after_gid
    deps.save()


def _install_zeek_package(deps):
    before_gid = _group_gid(deps)
    try:
        deps.install_packages(["zeek-9.0-core"])
    except Exception:
        _record_group(deps, before_gid, _group_gid(deps), required=False)
        raise
    _record_group(deps, before_gid, _group_gid(deps), required=True)


def ensure_zeek(deps):
    """Ensure Zeek 9+ is installed without replacing an existing runtime."""
    root = deps.path("/opt/zeek")
    binary = regular(deps.path("/opt/zeek/bin/zeek"))
    if binary.is_file():
        binary = _validate_runtime_path(deps, binary)
        if _finish_partial_install(deps):
            binary = _validate_runtime_path(deps, binary)
        return _version(deps, binary)

    directories = deps.data.setdefault("directories", [])
    if not isinstance(directories, list):
        raise TypeError("Invalid directory ownership state")
    if root.exists() or root.is_symlink():
        if not root.is_dir() or "/opt/zeek" not in directories:
            raise ValueError("Refusing to install into an unowned /opt/zeek directory")
    else:
        directories.append("/opt/zeek")
        deps.save()

    distribution = str(deps.data.get("distribution", ""))
    if distribution not in SUPPORTED_DISTRIBUTIONS:
        raise ValueError("Zeek packages are supported on Debian 12 and 13")
    architecture = _text(_run(deps, "dpkg", "--print-architecture").stdout).strip()
    if architecture not in SUPPORTED_ARCHITECTURES:
        raise ValueError(
            f"Zeek packages are unavailable for architecture {architecture}"
        )

    deps.install_packages(["ca-certificates", "curl"])
    repository = (
        "https://download.opensuse.org/repositories/security:/zeek/"
        f"Debian_{distribution}"
    )
    key_result = _run(
        deps,
        "curl",
        "--fail",
        "--silent",
        "--show-error",
        "--location",
        f"{repository}/Release.key",
        text=False,
    )
    key = key_result.stdout
    if not isinstance(key, bytes):
        key = _text(key).encode()
    if hashlib.sha256(key).hexdigest() != KEY_SHA256:
        raise ValueError(
            "Zeek repository key checksum changed; review the key rotation"
        )

    source = (
        f"deb [arch={architecture} signed-by={KEY_PATH}] {repository}/ /\n"
    ).encode()
    deps.own_file(KEY_PATH, key, mode=0o644)
    deps.own_file(SOURCE_PATH, source, mode=0o644)
    deps.save()

    _install_zeek_package(deps)

    if not binary.is_file():
        raise ValueError("Zeek package installed without /opt/zeek/bin/zeek")
    binary = _validate_runtime_path(deps, binary)
    return _version(deps, binary)

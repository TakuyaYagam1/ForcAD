"""Give the installing operator access without making secrets public."""

import json
import os
import pwd
import stat
import sys
from pathlib import Path

from .common import regular


def operator(previous=None):
    """Keep the original operator when another administrator reruns a script."""
    name = previous["name"] if previous else os.environ.get("SUDO_USER")
    if not name or name == "root":
        return None
    try:
        account = pwd.getpwnam(name)
    except KeyError:
        raise ValueError(f"Installation user no longer exists: {name}") from None
    result = {"name": name, "uid": account.pw_uid, "gid": account.pw_gid}
    if account.pw_uid == 0 or (previous and result != previous):
        raise ValueError(f"Installation user identity changed: {name}")
    if not previous and os.environ.get("SUDO_UID") not in (
        None,
        str(account.pw_uid),
    ):
        raise ValueError("SUDO_USER and SUDO_UID identify different users")
    return result


def own_tree(directory, owner, *, excluded=()):
    """Only visit real files on this filesystem; leave service data untouched."""
    if not owner:
        return
    regular(directory / "check")
    device = directory.stat().st_dev
    for current, directories, files in os.walk(directory, followlinks=False):
        parent = Path(current)
        directories[:] = [
            name
            for name in directories
            if not (parent / name).is_symlink()
            and not (parent == directory and name in excluded)
            and (parent / name).stat().st_dev == device
            and not (parent / name).is_mount()
        ]
        for path in (parent, *(parent / name for name in files)):
            info = path.lstat()
            if not (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)):
                continue
            if info.st_dev != device or (
                stat.S_ISREG(info.st_mode) and info.st_nlink > 1
            ):
                raise ValueError(f"Refusing shared installation file: {path}")
            os.chown(path, owner["uid"], owner["gid"], follow_symlinks=False)
            mode = stat.S_IMODE(info.st_mode) | (0o700 if path.is_dir() else 0o600)
            path.chmod(mode)


def checkout(directory, owner):
    regular(directory / ".forcad-fresh-install")
    if not (directory / ".forcad-fresh-install").is_file():
        raise ValueError(f"Not an installer-owned ForcAD checkout: {directory}")
    # Database files must retain their container UID and restrictive modes.
    own_tree(directory, owner, excluded=("docker_volumes",))


def main():
    if os.geteuid() != 0:
        raise ValueError("Run this command with sudo")
    directory = Path(sys.argv[1])
    installed = json.loads(regular(Path("/etc/forcad/forcad/install.json")).read_text())
    if str(directory) != installed["options"]["repo"]:
        raise ValueError("Checkout differs from the installed manifest")
    owner = operator(installed.get("owner"))
    checkout(directory, owner)
    if owner:
        print(f"Installation owner: {owner['name']}")


if __name__ == "__main__":
    main()

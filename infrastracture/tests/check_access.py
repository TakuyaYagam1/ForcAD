"""Check real UID permissions in a disposable Debian container."""

import os
import pwd
import stat
import subprocess
from pathlib import Path

from test_host import HostTests

from hostlib import access


def run_as(name, script, *arguments):
    return subprocess.run(
        ["runuser", "-u", name, "--", "sh", "-ec", script, "sh", *map(str, arguments)],
        check=False,
        capture_output=True,
        text=True,
    )


def main():
    if not Path("/.dockerenv").is_file() or os.geteuid() != 0:
        raise RuntimeError("Run this check inside its disposable root container")
    subprocess.run(["useradd", "-M", "-u", "1201", "organizator"], check=True)
    account = pwd.getpwnam("organizator")
    os.environ.update(SUDO_USER=account.pw_name, SUDO_UID=str(account.pw_uid))
    fixture = HostTests()
    fixture.setUp()
    try:
        fixture.work.chmod(0o755)
        fixture.root.chmod(0o755)
        interface = fixture.write(
            "/etc/network/interfaces", "auto lo\niface lo inet loopback\n"
        )
        original = interface.stat()
        wireguard = fixture.root / "etc/wireguard"
        wireguard.mkdir()
        wireguard.chmod(0o700)
        foreign = wireguard / "other.conf"
        foreign.write_text("existing private tunnel\n")
        foreign.chmod(0o600)
        installer, _ = fixture.vpn_configs()
        installer.plan()
        installer.install()
        assert wireguard.stat().st_uid == account.pw_uid
        for path in (interface, wireguard / "wg-admin.conf", installer.manifest):
            assert path.stat().st_uid == account.pw_uid
            assert path.stat().st_gid == account.pw_gid
        result = run_as(
            account.pw_name,
            'cd "$1"; test -r wg-admin.conf; test -w wg-admin.conf; '
            ": > operator-file; rm operator-file; cat wg-admin.conf >/dev/null",
            wireguard,
        )
        assert result.returncode == 0, result.stderr
        assert (
            run_as(
                "nobody", 'cat "$1" >/dev/null', wireguard / "wg-admin.conf"
            ).returncode
            != 0
        )
        assert foreign.stat().st_uid == 0
        installer.uninstall()
        assert (
            interface.stat().st_uid,
            interface.stat().st_gid,
            interface.stat().st_mode,
        ) == (original.st_uid, original.st_gid, original.st_mode)
        assert wireguard.stat().st_uid == 0
        assert stat.S_IMODE(wireguard.stat().st_mode) == 0o700
        assert foreign.read_text() == "existing private tunnel\n"

        checkout = fixture.work / "forcad"
        credentials = checkout / "config/runtime/admin.env"
        credentials.parent.mkdir(parents=True, mode=0o700)
        credentials.write_text("fixture\n")
        credentials.chmod(0o600)
        (checkout / ".forcad-fresh-install").write_text("fixture\n")
        database = checkout / "docker_volumes/postgres/data"
        database.mkdir(parents=True, mode=0o700)
        os.chown(database, 999, 999)
        access.checkout(checkout, access.operator())
        assert credentials.stat().st_uid == account.pw_uid
        assert database.stat().st_uid == 999
        assert (
            run_as(
                account.pw_name, 'test -r "$1"; test -w "$1"', credentials
            ).returncode
            == 0
        )
        assert run_as("nobody", 'cat "$1" >/dev/null', credentials).returncode != 0
        print(
            "PASS real user access, private credentials, service ownership and uninstall restoration"
        )
    finally:
        fixture.doCleanups()


if __name__ == "__main__":
    main()

"""Exercise real Debian packages in disposable containers, without host networking changes."""

import argparse
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from hostlib.packages import Dependencies


class Bundle:
    role = "router"
    root = Path("/")
    files_only = True

    @staticmethod
    def path(name):
        return Path(name)


class ContainerDependencies(Dependencies):
    @staticmethod
    def run(*args, **kwargs):
        # A disposable package test has no systemd PID 1. Lifecycle tests cover units.
        if args[0] == "systemctl":
            output = (
                "LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"
                if args[1] == "show"
                else ""
            )
            return subprocess.CompletedProcess(args, 0, stdout=output, stderr="")
        return Dependencies.run(*args, **kwargs)


def inside(role):
    if not Path("/.dockerenv").is_file() or os.geteuid() != 0:
        raise RuntimeError(
            "This package check must run inside its disposable Docker container"
        )
    bundle = Bundle()
    bundle.role = role
    deps = ContainerDependencies(bundle)
    before = deps.installed()
    resolver = Path("/etc/resolv.conf").read_bytes()
    deps.install(game_metrics=role == "router")
    assert deps.data["packages"]
    assert not (set(deps.data["packages"]) & before)
    first = set(deps.data["packages"])
    deps.install(game_metrics=role == "router")
    assert first == set(deps.data["packages"])
    if role == "router":
        deps.run(
            "/opt/zeek/bin/zeek", "-b", "-a", str(ROOT / "router/game-metrics.zeek")
        )
    else:
        deps.run("wg", "--version")
    deps.uninstall(choice="purge")
    assert not deps.directory.exists()
    assert not Path("/opt/zeek").exists()
    assert not Path("/etc/apt/sources.list.d/forcad-zeek.list").exists()
    assert not Path("/etc/apt/keyrings/forcad-zeek.asc").exists()
    assert Path("/etc/resolv.conf").read_bytes() == resolver
    assert before <= deps.installed()
    assert deps.run("getent", "passwd", "forcad-sensor", check=False).returncode == 2
    print(
        f"PASS {role} Debian dependencies: install, repeat, runtime check, purge, original packages and DNS preserved"
    )


def bootstrap_phase(action):
    if not Path("/.dockerenv").is_file() or os.geteuid() != 0:
        raise RuntimeError("Bootstrap checks require their disposable container")
    bundle = Bundle()
    bundle.role = "vpn"
    deps = ContainerDependencies(bundle)
    if action == "install":
        deps.install()
        assert "python3" in deps.data["packages"]
        deps.own_file("/etc/forcad-package-test", b"original\n")
    else:
        deps.uninstall(choice="purge")


BOOTSTRAP = r"""
test ! -x /usr/bin/python3
mkdir /test
cd /test
for action in install uninstall; do
    sed -e 's/@@ROLE@@/vpn/g' -e "s/@@ACTION@@/$action/g" \
        /work/infrastracture/templates/host.sh.in > "$action.sh"
done
cat > host.py <<'PY'
import sys
sys.path.insert(0, '/work/infrastracture/tests')
from run_dependencies import bootstrap_phase
bootstrap_phase(sys.argv[1])
PY
bash install.sh
printf 'changed\n' > /etc/forcad-package-test
if bash uninstall.sh --purge-packages; then
    echo 'Cleanup should reject the changed owned file' >&2
    exit 1
fi
test ! -x /usr/bin/python3
test -f /var/lib/forcad-dependencies/vpn/state.json
printf 'original\n' > /etc/forcad-package-test
bash uninstall.sh --purge-packages
test ! -x /usr/bin/python3
test ! -d /var/lib/forcad-dependencies/vpn
test ! -e /etc/forcad-package-test
echo 'PASS Python bootstrap, self-purge failure and uninstall recovery'
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inside", action="store_true")
    parser.add_argument("--debian", choices=("12", "13"), default="13")
    parser.add_argument("--role", choices=("router", "vpn"), default="router")
    parser.add_argument("--bootstrap", action="store_true")
    args = parser.parse_args()
    if args.inside:
        inside(args.role)
        return
    subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "--volume",
            f"{ROOT}:/work/infrastracture:ro",
            "--env",
            "PYTHONDONTWRITEBYTECODE=1",
            f"debian:{args.debian}-slim",
            "bash",
            "-ec",
            BOOTSTRAP
            if args.bootstrap
            else (
                "apt-get update && apt-get install -y --no-install-recommends python3 "
                f"&& python3 /work/infrastracture/tests/run_dependencies.py --inside --role {args.role}"
            ),
        ],
        check=True,
    )


if __name__ == "__main__":
    main()

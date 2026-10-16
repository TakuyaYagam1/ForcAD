"""Exercise portable installers inside the disposable network test container."""

import importlib.util
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from helpers import small_config

import render

spec = importlib.util.spec_from_file_location("generate", ROOT / "vpn/generate.py")
generate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generate)


def run(*args, check=True):
    result = subprocess.run(
        args, capture_output=True, text=True, check=False, timeout=30
    )
    if check and result.returncode:
        raise AssertionError(f"{args[0]} failed: {result.stderr}")
    return result


def snapshot():
    return [
        run(*command).stdout
        for command in (
            ("ip", "-j", "address"),
            ("ip", "-j", "route"),
            ("nft", "list", "ruleset"),
        )
    ] + [Path("/proc/sys/net/ipv4/ip_forward").read_text()]


def main():
    config = render.load_config(ROOT / "settings.example.toml")
    run(
        sys.executable,
        "-m",
        "unittest",
        "discover",
        "-s",
        str(ROOT / "tests"),
        "-p",
        "test_*.py",
    )
    small_config(config)
    bundle = Path("/work/bundle")
    render.write_bundle(bundle, render.render(config))
    generate.generate(config, bundle / "wireguard")
    shutil.copytree(bundle / "wireguard/server", bundle / "vpn/server")
    for number in range(19, 23):
        run(
            "ip",
            "link",
            "add",
            f"ens{number}",
            "type",
            "veth",
            "peer",
            "name",
            f"peer-{number}",
        )
    for role in ("router", "vpn"):
        Path("/etc/network/interfaces").write_text("auto lo\niface lo inet loopback\n")
        before = snapshot()
        directory = bundle / role
        stage = ["--files-only"]
        run("bash", str(directory / "install.sh"), *stage)
        assert snapshot() == before, "Installer changed live network state"
        result = run("bash", str(directory / "install.sh"), *stage)
        assert "Already installed" in result.stdout, result.stdout
        run("bash", str(directory / "test.sh"), *stage)
        result = run("bash", str(directory / "test.sh"), *stage, "--live", check=False)
        assert result.returncode != 0 and "Missing live address" in result.stderr
        assert snapshot() == before, "Checks changed live network state"
        print(
            f"PASS {role}: portable install, repeat install, static checks, unapplied state detection"
        )
        path = Path(
            f"/etc/forcad/{role}/{'common.nft' if role == 'router' else 'firewall.nft'}"
        )
        original = path.read_bytes()
        path.write_bytes(original + b"\n# drift\n")
        result = run("bash", str(directory / "test.sh"), *stage, check=False)
        assert result.returncode != 0 and "Installed file differs" in result.stderr
        result = run("bash", str(directory / "uninstall.sh"), *stage, check=False)
        assert result.returncode != 0 and path.read_bytes() != original
        run("bash", str(directory / "uninstall.sh"), *stage, "--force", "--dry-run")
        assert path.exists()
        run("bash", str(directory / "uninstall.sh"), *stage, "--force")
        assert not path.exists()
        assert (
            Path("/etc/network/interfaces").read_text()
            == "auto lo\niface lo inet loopback\n"
        )
        assert snapshot() == before, "Uninstaller changed live network state"
        run("bash", str(directory / "uninstall.sh"), *stage)
        print(
            f"PASS {role}: uninstall, drift protection, forced backup, repeated uninstall; live state unchanged"
        )


if __name__ == "__main__":
    main()

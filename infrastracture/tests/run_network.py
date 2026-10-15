"""Run packet checks in disposable containers, never in the host namespace."""

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
import render


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--image", help="Reuse an existing test image instead of building one"
    )
    args = parser.parse_args()
    image = args.image or "forcad-network-test"
    built = False
    if not args.image:
        exists = subprocess.run(
            ["docker", "image", "inspect", image], capture_output=True, check=False
        )
        if exists.returncode == 0:
            parser.error(f"Image {image} already exists; use --image to reuse it")
    try:
        if not args.image:
            run("docker", "build", "-t", image, str(ROOT / "tests"))
            built = True
        with tempfile.TemporaryDirectory(prefix="forcad-network-") as directory:
            scratch = Path(directory)
            bundle = scratch / "bundle"
            config = render.load_config(ROOT / "network.example.toml")
            config.update(teams=2, game_udp_ports=[9000])
            config["router"].update(
                wan_interface="eth0",
                vpn_interface="eth1",
                forcad_interface="eth2",
                team_trunk="eth3",
            )
            config["vpn"]["interface"] = "eth0"
            render.write_bundle(bundle, render.render(config))
            for role in ("router", "vpn"):
                cid = scratch / f"{role}.cid"
                try:
                    run(
                        "docker",
                        "run",
                        "--rm",
                        "--cidfile",
                        str(cid),
                        "--network",
                        "none",
                        "--cap-drop",
                        "ALL",
                        "--cap-add",
                        "NET_ADMIN",
                        "--cap-add",
                        "NET_RAW",
                        "--sysctl",
                        "net.ipv4.ip_forward=1",
                        "--sysctl",
                        "net.ipv4.conf.all.rp_filter=2",
                        "--sysctl",
                        "net.ipv4.conf.default.rp_filter=2",
                        "--mount",
                        f"type=bind,src={bundle},dst=/etc/forcad,readonly",
                        "--mount",
                        f"type=bind,src={ROOT},dst=/source,readonly",
                        image,
                        role,
                        timeout=90,
                    )
                finally:
                    if cid.exists():
                        subprocess.run(
                            ["docker", "rm", "-f", cid.read_text().strip()],
                            capture_output=True,
                            check=False,
                        )
    finally:
        if built:
            run("docker", "image", "rm", image)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Create a new private WireGuard bundle without changing any running host."""

import argparse
import csv
import io
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from render import ROOT, load_config, teams, write_bundle


def wg(command, value=None):
    # Never log subprocess output: it may contain a private key.
    return subprocess.run(
        ["wg", command], input=value, text=True, capture_output=True, check=True
    ).stdout.strip()


def keypair():
    private = wg("genkey")
    return private, wg("pubkey", private + "\n")


def server(private, address, port, peers):
    content = f"[Interface]\nPrivateKey = {private}\nAddress = {address}/24\nListenPort = {port}\n"
    for name, peer_address, public, psk in peers:
        content += f"\n# {name}\n[Peer]\nPublicKey = {public}\nPresharedKey = {psk}\nAllowedIPs = {peer_address}/32\n"
    return content


def client(private, address, public, psk, endpoint, port):
    return f"""[Interface]
PrivateKey = {private}
Address = {address}/32

[Peer]
PublicKey = {public}
PresharedKey = {psk}
Endpoint = {endpoint}:{port}
AllowedIPs = 10.183.0.0/16
PersistentKeepalive = 25
"""


def generate(config, output):
    # Refuse an existing destination before generating any keys.
    if output.exists() or output.is_symlink():
        raise FileExistsError(output)
    attack_private, attack_public = keypair()
    admin_private, admin_public = keypair()
    attack_peers, admin_peers, files = [], [], {}
    manifest = io.StringIO()
    rows = csv.writer(manifest)
    rows.writerow(("team", "member", "subnet", "address", "config", "public_key"))
    for team in teams(config):
        for member, address in enumerate(team["peers"], 1):
            name = f"{team['name']}/member-{member:02}"
            private, public = keypair()
            psk = wg("genpsk")
            attack_peers.append((name, address, public, psk))
            relative = f"participants/{name}.conf"
            files[relative] = client(
                private,
                address,
                attack_public,
                psk,
                config["vpn"]["endpoint"],
                config["vpn"]["participant_port"],
            )
            rows.writerow(
                (
                    team["id"],
                    member,
                    team["vpn_cidr"],
                    address,
                    relative,
                    public,
                )
            )
    for number in range(1, config["admins"] + 1):
        name, address = f"admin-{number:02}", f"10.183.201.{number + 1}"
        private, public = keypair()
        psk = wg("genpsk")
        admin_peers.append((name, address, public, psk))
        files[f"admins/{name}.conf"] = client(
            private,
            address,
            admin_public,
            psk,
            config["vpn"]["endpoint"],
            config["vpn"]["admin_port"],
        )
    files["server/wg-attack.conf"] = server(
        attack_private, "10.183.200.1", config["vpn"]["participant_port"], attack_peers
    )
    files["server/wg-admin.conf"] = server(
        admin_private, "10.183.201.1", config["vpn"]["admin_port"], admin_peers
    )
    files["assignments.csv"] = manifest.getvalue()
    write_bundle(output, files)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=ROOT / "network.toml")
    parser.add_argument("--output", type=Path, default=ROOT / "runtime/wireguard")
    args = parser.parse_args()
    try:
        config = load_config(args.config)
        generate(config, args.output)
    except (ValueError, TypeError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(
            1,
            f"WireGuard generation failed ({type(error).__name__}). Existing output is never overwritten.\n",
        )
    print(
        f"Created private WireGuard configs in {args.output}. Keys were not applied or printed."
    )


if __name__ == "__main__":
    main()

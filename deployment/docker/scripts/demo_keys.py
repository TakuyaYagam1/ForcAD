"""Extend a demo VPN without rotating the existing peers' keys."""

import csv
import io
import re
import shutil

import render
from vpn.generate import client, generate, keypair, wg


def prepare_keys(config, output):
    wireguard = output / "wireguard"
    if not wireguard.exists():
        generate(config, wireguard)
    manifest = wireguard / "assignments.csv"
    rows = list(csv.DictReader(io.StringIO(manifest.read_text())))
    expected = {
        (str(team["id"]), str(member)): (team, address)
        for team in render.teams(config)
        for member, address in enumerate(team["peers"], 1)
    }
    by_member = {}
    for row in rows:
        member = (row["team"], row["member"])
        path = wireguard / row["config"]
        if (
            member not in expected
            or member in by_member
            or path.is_symlink()
            or not path.resolve().is_relative_to(wireguard.resolve())
            or not path.is_file()
            or row["address"] != expected[member][1]
        ):
            raise SystemExit("Existing demo VPN inventory conflicts with settings.toml")
        by_member[member] = row
    allowed = ", ".join(map(str, render.network_layout(config)["allowed_networks"]))
    server_path = wireguard / "server/wg-attack.conf"
    server = server_path.read_text()
    updates = {}
    missing = expected.keys() - by_member.keys()
    if missing:
        private = re.search(r"^PrivateKey = (.+)$", server, re.MULTILINE)
        if private is None:
            raise SystemExit("Existing demo VPN server has no private key")
        public = wg("pubkey", private[1] + "\n")
        for member in sorted(missing, key=lambda item: tuple(map(int, item))):
            team, address = expected[member]
            private, peer_public = keypair()
            psk = wg("genpsk")
            relative = f"participants/{team['name']}/member-{int(member[1]):02}.conf"
            if (wireguard / relative).exists():
                raise SystemExit(
                    "Untracked demo VPN client exists; restore its manifest first"
                )
            updates[wireguard / relative] = client(
                private,
                address,
                public,
                psk,
                config["vpn"]["endpoint"],
                config["vpn"]["participant_port"],
                allowed,
            )
            server += (
                f"\n[Peer]\nPublicKey = {peer_public}\nPresharedKey = {psk}\n"
                f"AllowedIPs = {address}/32\n"
            )
            row = dict(
                zip(
                    ("team", "member", "subnet", "address", "config", "public_key"),
                    (*member, team["vpn_cidr"], address, relative, peer_public),
                )
            )
            rows.append(row)
            by_member[member] = row
        updates[server_path] = server
    for path in [
        *wireguard.glob("participants/**/*.conf"),
        *wireguard.glob("admins/*.conf"),
    ]:
        original = path.read_text()
        content = re.sub(
            r"^AllowedIPs = .+$",
            "AllowedIPs = " + allowed,
            original,
            flags=re.MULTILINE,
        )
        if content != original:
            updates[path] = content
    if updates:
        # Keep the last complete bundle, including the assignment manifest.
        backup = output / f"wireguard-before-{len(expected)}-peers"
        if not backup.exists():
            shutil.copytree(wireguard, backup)
        for path, content in updates.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
            path.chmod(0o600)
        buffer = io.StringIO()
        writer = csv.DictWriter(buffer, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
        manifest.write_text(buffer.getvalue())
    players = output / "players"
    players.mkdir(mode=0o700, exist_ok=True)
    for member, row in by_member.items():
        if member[1] == "1":
            destination = players / f"team{member[0]}.conf"
            shutil.copyfile(wireguard / row["config"], destination)
            destination.chmod(0o600)

"""Command line entry point for a role-specific host bundle."""

import argparse
import json
import os
import subprocess
from pathlib import Path

from .common import regular


def main(bundle_class, directory):
    parser = argparse.ArgumentParser(description="Manage files for this host role")
    parser.add_argument("action", choices=("install", "test", "uninstall"))
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Preview uninstall without changing files",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Back up later edits and restore original files during uninstall",
    )
    parser.add_argument(
        "--repo", type=Path, help="Existing ForcAD checkout (absolute path)"
    )
    parser.add_argument(
        "--wireguard-dir", type=Path, help="Directory containing server configs only"
    )
    parser.add_argument(
        "--network-metrics",
        action="store_true",
        help="Install the network exporter or its Prometheus target",
    )
    parser.add_argument(
        "--game-metrics",
        action="store_true",
        help="Install the Zeek sensor or its Prometheus target",
    )
    parser.add_argument(
        "--live",
        action="store_true",
        help="Also check applied settings and running services",
    )
    parser.add_argument(
        "--files-only",
        action="store_true",
        help="Use file-only operations for the router or VPN host",
    )
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.exit(
            1,
            "Run this command through install.sh, test.sh or uninstall.sh with sudo access.\n",
        )
    os.umask(0o077)
    try:
        bundle = bundle_class(Path(directory))
        if args.files_only and bundle.role not in {"router", "vpn"}:
            raise ValueError(
                "--files-only is only supported by router and VPN installers"
            )
        bundle.files_only = args.files_only
        if args.action == "uninstall":
            if (
                args.live
                or args.repo
                or args.wireguard_dir
                or args.network_metrics
                or args.game_metrics
            ):
                raise ValueError(
                    "uninstall.sh reads its history from the installed manifest; use only --dry-run, --force and/or --files-only"
                )
            bundle.uninstall(force=args.force, dry_run=args.dry_run)
            return
        if args.dry_run or args.force:
            raise ValueError("--dry-run and --force are only supported by uninstall.sh")
        if args.action == "install" and args.live:
            raise ValueError("--live is only supported by test.sh")
        if args.action == "test" and (
            args.repo or args.wireguard_dir or args.network_metrics or args.game_metrics
        ):
            raise ValueError(
                "test.sh reads installation options from the installed manifest; use only --live and/or --files-only"
            )
        if bundle.role != "forcad" and args.repo:
            raise ValueError("--repo is only supported by the ForcAD installer")
        if bundle.role != "vpn" and args.wireguard_dir:
            raise ValueError("--wireguard-dir is only supported by the VPN installer")
        if bundle.role == "vpn" and (args.game_metrics or args.network_metrics):
            raise ValueError("Monitoring exporters run on the router, not the VPN host")
        if args.action == "install":
            if bundle.manifest.exists():
                previous = json.loads(regular(bundle.manifest).read_text())["options"]
                args.repo = args.repo or previous.get("repo")
                args.network_metrics |= previous.get("network_metrics", False)
                args.game_metrics |= previous.get("game_metrics", False)
            bundle.plan(
                args.repo, args.wireguard_dir, args.network_metrics, args.game_metrics
            )
            bundle.validate()
            bundle.install()
        else:
            live = args.live or (bundle.role == "router" and not args.files_only)
            bundle.test(live=live)
    except (
        OSError,
        ValueError,
        TypeError,
        KeyError,
        subprocess.SubprocessError,
    ) as error:
        parser.exit(1, f"FAIL {error}\n")

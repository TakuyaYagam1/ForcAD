"""Manage the networking dependency while stopping an owned firewall."""

from .common import regular, run
from .files import atomic_write, file_state


def stop_firewall(bundle, unit):
    if unit not in {
        "forcad-router-firewall.service",
        "forcad-vpn-firewall.service",
    }:
        raise ValueError("Not a managed firewall unit")
    path = regular(bundle.path("/etc/systemd/system/networking.service.d/forcad.conf"))
    state = file_state(path)
    data = path.read_bytes() if state["existed"] else None
    expected = f"[Unit]\nRequires={unit}\nAfter={unit}\n".encode()
    if data is not None and data != expected:
        raise ValueError(f"Cannot detach custom networking configuration: {path}")
    try:
        if data is not None:
            path.unlink()
        run("systemctl", "daemon-reload")
        dependencies = run(
            "systemctl",
            "show",
            "networking.service",
            "--property=Requires",
            "--value",
        ).split()
        if unit in dependencies:
            raise ValueError(
                f"networking.service still requires {unit} through another configuration"
            )
        run("systemctl", "stop", unit)
    finally:
        if data is not None:
            atomic_write(path, data, state["mode"], state["uid"], state["gid"])
            run("systemctl", "daemon-reload")

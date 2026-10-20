# VPN server

Use a dedicated Debian 12/13 host with `python3`, `wireguard-tools`,
`nftables`, `iproute2`, `iputils-ping`, `procps`, `ifupdown` and `resolvconf`.
The installer does not install packages. The transit subnet and both host
addresses are generated from `[network]` in `settings.toml`. Start with the
[generator guide](../README.md) for the complete workflow.

## Generate keys

After editing `settings.toml` and running `python3 render.py`, generate keys
once on the workstation:

```sh
python3 vpn/generate.py
```

This requires local `wireguard-tools`. It creates server configs, participant
and admin configs, and `assignments.csv` under `runtime/wireguard/`. It also
updates `runtime/vpn.tar.gz` with the two server configs. The archive contains
private server keys; keep it private. Send each client config only to its
owner.

Do not generate keys again when changing firewall or network settings. The
generator refuses an existing output directory; a new output creates new peer
identities. The installer also refuses to replace existing server keys with
different keys. Keep the deployed server configs for subsequent renders. Place
them in `server/` beside the extracted bundle or pass
`--wireguard-dir /path/to/server` to `install.sh`. Without either, the installer
can use existing configs in `/etc/wireguard/`. Check that peer addresses still
match the new settings.

## Install

Copy `runtime/vpn.tar.gz` to the VPN host and extract it into a new `vpn/`
directory. Run from there at the host console:

```sh
sudo bash install.sh
sudo bash test.sh
```

The installer applies the interface and sysctl settings, loads the VPN
firewall, and enables both WireGuard tunnels. It records original live state
and files for rollback. It does not install OS packages. Keep console access:
networking may restart if the address or default route changes.

The router must forward the configured participant and admin UDP ports to this
host. Its return routes for both VPN ranges must use the VPN host address on the
transit link. The VPN host preserves peer addresses and does not perform SNAT.

`test.sh` checks configuration, services, routes and configured connectivity.
Use `install.sh --files-only` and `test.sh --files-only` to stage and check files
without applying live settings. Client handshakes and access isolation require
a connected client; verify those separately.

## Remove

Preview and restore the saved state from the host console:

```sh
sudo bash uninstall.sh --dry-run
sudo bash uninstall.sh
```

If files changed after installation, review the diff before using
`sudo bash uninstall.sh --force`. See [UNINSTALL.txt](UNINSTALL.txt).

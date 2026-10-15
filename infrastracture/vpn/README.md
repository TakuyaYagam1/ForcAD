# VPN server

Debian 12/13 with `wireguard-tools`, `nftables`, `iproute2`, `ifupdown` and
`resolvconf`. Address: `10.183.1.2/30`, gateway: `10.183.1.1`. The router forwards
UDP/56767 and UDP/56768 and routes both VPN networks through this host.

## Generate keys

After [rendering configs](../README.md#generate-configs), run on the workstation:

```sh
python3 infrastracture/vpn/generate.py
```

Output: `infrastracture/runtime/wireguard/` with `server/`,
`participants/team-NN/`, `admins/` and public `assignments.csv`.

Each peer has its own key, preshared key and server-side `/32` AllowedIPs.
Clients route only `10.183.0.0/16` through VPN; check for conflicts with their local
networks. Public Internet uses their normal connection.

Generate once for a new deployment. Existing output is refused; generating into
another directory rotates all identities. Keep deployed keys when updating
firewall or interface configs. Distribute only each recipient's own config.

## Install

Keep console access and back up current configs. Merge `runtime/vpn/interfaces`
into `/etc/network/interfaces`, preserving management access. Use one network
manager per device and resolvconf or an explicitly configured DNS resolver.

Copy `runtime/vpn/` and only `runtime/wireguard/server/` to the VPN host as sibling
`vpn/` and `server/` directories. Run from `vpn/`:

```sh
sudo install -d -m 0750 /etc/forcad/vpn
sudo install -m 0600 firewall.nft /etc/forcad/vpn/firewall.nft
sudo install -m 0644 sysctl.conf /etc/sysctl.d/90-forcad-vpn.conf
sudo install -m 0644 forcad-vpn-firewall.service /etc/systemd/system/
sudo install -d /etc/systemd/system/networking.service.d
sudo install -m 0644 networking.conf /etc/systemd/system/networking.service.d/forcad.conf
sudo install -d -m 0700 /etc/wireguard
sudo install -m 0600 ../server/wg-attack.conf ../server/wg-admin.conf /etc/wireguard/
sudo install -d /etc/systemd/system/wg-quick@wg-attack.service.d
sudo install -d /etc/systemd/system/wg-quick@wg-admin.service.d
sudo install -m 0644 firewall.conf /etc/systemd/system/wg-quick@wg-attack.service.d/firewall.conf
sudo install -m 0644 firewall.conf /etc/systemd/system/wg-quick@wg-admin.service.d/firewall.conf
sudo nft -c -f /etc/forcad/vpn/firewall.nft
sudo systemctl daemon-reload
sudo systemctl enable --now forcad-vpn-firewall.service
sudo sysctl -p /etc/sysctl.d/90-forcad-vpn.conf
```

Apply the transit interface config from the console, then start tunnels:

```sh
sudo systemctl enable --now wg-quick@wg-attack wg-quick@wg-admin
sudo wg show
```

Drop-ins load the firewall before networking and either tunnel. No nftables
PostUp/PostDown hooks are needed. Reconcile other firewall managers; none may
flush these rules at boot.

## Maintenance

- Keep original peer IPs: no NAT on the VPN server. The router uses them for
  ownership checks, SNAT and metrics.
- Same-team `/29` groups can communicate; other teams and the admin VPN are
  isolated. Server-side AllowedIPs must remain unique `/32`s.
- For a firewall update, validate with `nft -c -f`, then reload
  `forcad-vpn-firewall.service`. Only `inet forcad_vpn` is replaced; keys and
  tunnels stay intact.
- Revoke a peer in both the live interface and persistent server config.
  Do not share `wg show ... dump` output: it contains private keys.

Verify handshakes, same-team access, cross-team/admin isolation, board access and
both [game modes](../README.md#access-policy). Repeat after reboot. Roll back from
the console using saved interfaces, firewall and server configs; restart affected
units.

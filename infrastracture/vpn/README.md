# VPN

Generate the bundle with the [main guide](../README.md). See [commands and
options](../COMMANDS.md#vpn).

## Host and keys

Use a dedicated Debian 12 or 13 host. The installer adds missing packages,
including WireGuard tools, nftables and ifupdown. Keep console access because
network changes can interrupt SSH.

Rendering generates `wg-attack` and `wg-admin` server configs and client
configs. The VPN archive contains the server configs. Client configs and
`assignments.csv` stay under `runtime/wireguard/` on the workstation; give each
client only its own config. Keep archives and keys private.

Every render into a new output directory creates new server and client keys.
For the same deployment, preserve the deployed server configs and pass their
directory to `install.sh --wireguard-dir PATH`; their peer allocations must
match the bundle. If neither bundled nor selected configs are available, the
installer may adopt matching `wg-attack.conf` and `wg-admin.conf` from
`/etc/wireguard`, but refuses different keys.

## Install and remove

The installer applies networking, sysctl and firewall, then starts both tunnels.
The VPN host preserves peer addresses without SNAT. The Router must route both
VPN ranges through this host; see the [Router
guide](../router/README.md#firewall-and-removal) for WAN forwarding.

Participant peers may reach peers in their own team's VPN block and the Router
transit. The VPN firewall blocks participant traffic to other team blocks and
the admin VPN. Admin peers are not subject to those participant restrictions.

The test checks installed files, tunnels, routes and configured connectivity.
Verify an actual client handshake and access isolation separately. Uninstall
stops both managed tunnels and removes their server configs, including configs
adopted from `/etc/wireguard/`. Other WireGuard configs remain untouched. Keep a
private copy of the deployed server configs if you may reinstall.

## Optional observers

Install the base VPN bundle first, then enable either observer independently:

```sh
sudo ./install.sh --pcap
sudo ./install.sh --wg-endpoints
```

Observer installation only stages files and reloads systemd's unit definitions.
It does not start or enable an observer, restart WireGuard, regenerate keys, or
apply networking. The pcap option installs Debian's `wireshark-common` and
`tshark`; endpoint history uses the installed Python and WireGuard tools.

### VPN packet capture

Start and inspect capture manually:

```sh
sudo systemctl start forcad-vpn-pcap.service
sudo python3 /usr/lib/forcad/vpn/vpnlib/observers.py pcap-status
```

The service runs one continuous `dumpcap` on `wg-attack`, writes classic pcap
with UTC filenames, and captures both directions only between known WireGuard
peer addresses and exact vulnbox addresses in the installed inventory. The NPC
and SSH destinations are included when they are inventory vulnboxes. It is not
an all-interface or all-host capture.

Capture data is private under `/var/lib/forcad-vpn-pcap` (root-only directory).
The active ring has 96 files, rotates every 300 seconds while idle and also
rotates at 4,000,000 kB per file. Up to 96 files from the preceding run are
retained, for at most 192 capture files total (a conservative bound of about
786.5 GB / 732 GiB including one maximum-packet overshoot per file). If each
five-minute window writes less than the size limit, 96 slots cover about eight
hours. A 100 Mbit/s **stored** rate consumes 3.75 GB per window. At higher
rates one window can use multiple slots even if the disk still has space.
The preflight requires about 377 GiB (405 GB) free: a complete current ring, the 512 MiB
export allowance, and a 10 GiB filesystem reserve. The size is fixed. If that
free-space preflight fails, capture does not start and existing segments are
not pruned. The service fails closed rather than filling the filesystem.

`pcap-status` reports used space, the ring budget, free space, oldest retained
segment, and the latest completed dumpcap drop counters. While capture is
running its current drop counters are explicitly reported as not yet finalized.
Those counters become available after dumpcap exits.

Export only a single TCP or UDP 5-tuple and inclusive UTC epoch-millisecond
range (at most six hours); the filter is bidirectional and is built from typed
addresses and ports:

```sh
sudo python3 /usr/lib/forcad/vpn/vpnlib/observers.py pcap-extract \
  --protocol tcp --source <peer-ip> --source-port <port> \
  --destination <vulnbox-ip> --destination-port <port> \
  --from-ms <epoch-ms> --to-ms <epoch-ms> --name <case-name>
```

Exports are private in `/var/lib/forcad-vpn-pcap/exports`, limited to 64 MiB
each and eight files / 512 MiB total. Existing exports are not automatically
deleted. No matching segments or packets is an error and does not publish an
empty case file. Non-initial IP fragments without a transport header cannot
match the exact port filter; packets larger than the 262,144-byte snap length
are truncated. A segment still being written may lack trailing frames, so use
completed segments for a final export and do not infer completeness from a
successful narrow export alone. Drop counters should be considered with any
export.

### Authenticated WireGuard endpoint history

Start and stop this observer manually:

```sh
sudo systemctl start forcad-wg-endpoints.service
sudo systemctl stop forcad-wg-endpoints.service
```

It polls `wg-attack` every five seconds using only allowed IPs, endpoints, and
latest handshakes. It records only an exact inventory `/32` peer with a
valid endpoint and a handshake no older than 180 seconds. It stores UTC
observation seconds and the external IP, not the endpoint port or WireGuard
keys. Each service run begins with a `baseline`; later newly seen peers are
`first`, and `change` means that peer's external IP changed. Same-IP port
rebinding does not create a change. A stable peer with a fresh handshake gets
at most one baseline refresh per day, keeping long-running active history
bounded without treating an old cached handshake as activity.

The database is
`/var/lib/forcad-wg-endpoints/history.sqlite3`, in a root-only directory, with
a 30-day / 100,000-row retention limit and a 64 MiB database bound. Endpoint
data is never written to service stdout or metrics. This is address history,
not geolocation or proof of a participant's physical location.

Optional venue classification reads one CIDR per line from
`/etc/forcad/vpn/endpoint-venues.txt`. Include every venue network, including
backup networks. A missing, empty, or invalid list produces `unknown`;
addresses outside a valid list are `outside`. No geolocation lookup is made.

The history viewer is activated only through the local Unix socket
`/run/forcad-wg-history.sock` (root:`adm`, mode `0660`). Forward it over SSH
from an operator account in the remote `adm` group:

```sh
ssh -N -L 127.0.0.1:9837:/run/forcad-wg-history.sock operator@vpn-host
```

Then use the private browser view at
`http://127.0.0.1:9837/?peer=<inner-IP>&from=<epoch-ms>&to=<epoch-ms>`.
Uninstall stops any active observer units owned by this VPN bundle, removes
their unit files, and preserves capture and endpoint history for operator
retention or cleanup.

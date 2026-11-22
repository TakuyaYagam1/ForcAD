FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

# Debian 12 is the oldest supported host and exercises nftables 1.0.6.
# Root is required by run_network.py; see the scoped exception in security/trivy/ignore.yml.
RUN apt-get update \
    && printf '%s\n' 'resolvconf resolvconf/linkify-resolvconf boolean false' | debconf-set-selections \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends python3 python3-yaml nftables iproute2 iputils-ping procps ifupdown resolvconf vlan wireguard-tools git openssl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /work
ENV PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["python3", "/source/infrastracture/tests/packets.py"]

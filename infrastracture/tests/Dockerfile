FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587

# Debian 12 is the oldest supported host and exercises nftables 1.0.6.
# Root is required by run_network.py; see the scoped exception in security/trivy/ignore.yml.
RUN apt-get update \
    && printf '%s\n' 'resolvconf resolvconf/linkify-resolvconf boolean false' | debconf-set-selections \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends python3 python3-yaml nftables iproute2 iputils-ping procps ifupdown resolvconf vlan wireguard-tools \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /work
ENV PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["python3", "/source/infrastracture/tests/packets.py"]

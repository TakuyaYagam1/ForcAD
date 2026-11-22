# syntax=docker/dockerfile:1.29.0@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
# Digests from the signed Debian 13 package index used by the router installer.
FROM scratch AS package-amd64
ADD --checksum=sha256:f24b10d2afdd1f8e69fcbfc94e319aec95a683908beed4dcb6fd340e5527ebb2 https://download.opensuse.org/repositories/security:/zeek/Debian_13/amd64/zeek-9.0-core_9.0.1-0_amd64.deb /zeek.deb

FROM scratch AS package-arm64
ADD --checksum=sha256:5db3c6d42686c3d8e9cecaad174d5aec84637a2335f6eb0ee58c7fad6bb10ce0 https://download.opensuse.org/repositories/security:/zeek/Debian_13/arm64/zeek-9.0-core_9.0.1-0_arm64.deb /zeek.deb

FROM package-${TARGETARCH} AS package
FROM debian:trixie-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
RUN --mount=from=package,source=/zeek.deb,target=/tmp/zeek.deb \
    apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y --no-install-recommends \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends /tmp/zeek.deb curl \
    && rm -rf /var/lib/apt/lists/*
ENV PATH="/opt/zeek/bin:${PATH}"
USER 10001:10001
ENTRYPOINT ["/opt/zeek/bin/zeek"]

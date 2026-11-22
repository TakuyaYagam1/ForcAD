# syntax=docker/dockerfile:1.29.0@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
FROM app AS backend
FROM python:3.15.2-alpine3.24@sha256:0f9552bb28711333da16915ec146f5e6bb44a7fdac9ced15dc17c2e0206346dd
RUN apk add --no-cache iproute2 nftables setpriv wireguard-tools iputils
WORKDIR /app
COPY --from=backend /usr/local/bin/forcad /usr/local/bin/forcad
COPY deployment/demo/*.py ./
COPY deployment/docker/scripts/demo_entrypoint.sh /usr/local/bin/demo-entrypoint
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
USER 10001:10001
ENTRYPOINT ["sh", "/usr/local/bin/demo-entrypoint"]

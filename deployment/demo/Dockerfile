# syntax=docker/dockerfile:1.7
FROM app AS backend
FROM python:3.14.8-alpine3.24@sha256:f6a589d43c42b9e7f7dc67a12d37132491f362859a5d750607710cc56da3bc72
RUN apk add --no-cache iproute2 nftables setpriv
WORKDIR /app
COPY --from=backend /usr/local/bin/forcad /usr/local/bin/forcad
COPY deployment/demo/service.py deployment/demo/traffic.py ./
COPY deployment/docker/scripts/demo_entrypoint.sh /usr/local/bin/demo-entrypoint
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
USER 10001:10001
ENTRYPOINT ["sh", "/usr/local/bin/demo-entrypoint"]

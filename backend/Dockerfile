# syntax=docker/dockerfile:1.7

FROM ghcr.io/astral-sh/uv:0.13.0@sha256:cdc6093146eb3ff6a40107b38f008b789e050e77ad87865e381d9917da55a168 AS uv

FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie@sha256:604106102d33b1084e37ca08adf98ac0813624ae8595bd0d7f3965ecebded09c AS go-build

WORKDIR /src

ENV GOTOOLCHAIN=local

COPY backend/go.mod backend/go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY backend/cmd/ ./cmd/
COPY backend/config/ ./config/
COPY backend/db/ ./db/
COPY backend/internal/ ./internal/

ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -mod=readonly -trimpath -buildvcs=false \
      -ldflags='-s -w' -o /out/forcad ./cmd/forcad

FROM go-build AS test

CMD ["go", "test", "-mod=readonly", "./..."]

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS runtime

ENV HOME=/home/forcad

RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 forcad \
    && adduser -S -D -u 10001 -G forcad -h /home/forcad -s /sbin/nologin forcad \
    && chmod 0750 /home/forcad

WORKDIR /app

COPY --from=go-build /out/forcad /usr/local/bin/forcad
COPY deployment/docker/scripts/stage_config.sh /usr/local/bin/stage_config

RUN chmod 0755 /usr/local/bin/forcad /usr/local/bin/stage_config

USER forcad:forcad

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["forcad", "health"]

ENTRYPOINT ["forcad"]

FROM runtime AS initializer

CMD ["init"]

FROM python:3.14.8-alpine3.24@sha256:f6a589d43c42b9e7f7dc67a12d37132491f362859a5d750607710cc56da3bc72 AS checker-base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    HOME=/home/forcad

RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates libpq util-linux-misc \
    && addgroup -S -g 10001 forcad \
    && adduser -S -D -u 10001 -G forcad -h /home/forcad -s /sbin/nologin forcad \
    && chmod 0750 /home/forcad

# Dependencies are installed by uv in the build stage; runtime checkers do not
# need pip or its bundled copies of third-party libraries.
RUN python -m pip uninstall --yes pip

FROM checker-base AS checker-build

RUN apk add --no-cache build-base libffi-dev libpq-dev

COPY --from=uv /uv /uvx /usr/local/bin/
COPY checkers/requirements.txt /tmp/checker-requirements.txt

ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy

RUN uv venv /opt/checkers --python /usr/local/bin/python

RUN --mount=type=cache,target=/root/.cache/uv \
    uv pip install --python /opt/checkers/bin/python -r /tmp/checker-requirements.txt

FROM checker-base AS worker

COPY --from=go-build /out/forcad /usr/local/bin/forcad
COPY --from=checker-build /opt/checkers /opt/checkers

ENV PATH="/opt/checkers/bin:${PATH}"

USER forcad:forcad

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["forcad", "health"]

ENTRYPOINT ["forcad"]

FROM runtime AS default

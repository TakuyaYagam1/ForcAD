# syntax=docker/dockerfile:1.29.0@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183

FROM ghcr.io/astral-sh/uv:0.13.9@sha256:d3b601f0c7b118bc1753febf7c41cc80981df678445e0254284eecf84db79a7d AS uv

FROM --platform=$BUILDPLATFORM golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d AS go-build

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

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS runtime

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

FROM python:3.15.2-alpine3.24@sha256:0f9552bb28711333da16915ec146f5e6bb44a7fdac9ced15dc17c2e0206346dd AS checker-base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    HOME=/home/forcad

RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates libpq util-linux-misc \
    && addgroup -S -g 10001 forcad \
    && adduser -S -D -u 10001 -G forcad -h /home/forcad -s /sbin/nologin forcad \
    && chmod 0750 /home/forcad

# Dependencies are installed by uv in the build stage; runtime checkers do not
# need pip or its bundled copies of third-party libraries. The upstream image
# omits stdlib bytecode. Compile it once while building because runtime checkers
# cannot write caches and each action starts a fresh interpreter.
RUN python -m pip uninstall --yes pip \
    && python -m compileall -q "$(python -c 'import sysconfig; print(sysconfig.get_path("stdlib"))')"

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

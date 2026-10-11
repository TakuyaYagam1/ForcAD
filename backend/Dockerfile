# syntax=docker/dockerfile:1.7

FROM ghcr.io/astral-sh/uv:0.13.0@sha256:cdc6093146eb3ff6a40107b38f008b789e050e77ad87865e381d9917da55a168 AS uv

FROM python:3.14.8-slim-trixie@sha256:a2b82f3c48559aa0a8446d9af49826b6e2b2016f4cd2afabfe6013ec53729170 AS runtime-base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    HOME=/home/forcad

RUN apt-get update \
    && apt-get upgrade -y --no-install-recommends \
    && apt-get install -y --no-install-recommends libpq5 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 forcad \
    && useradd --uid 10001 --gid forcad --create-home --shell /usr/sbin/nologin forcad \
    && chmod 0750 /home/forcad

WORKDIR /app

FROM runtime-base AS python-build

RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev libpq-dev \
    && rm -rf /var/lib/apt/lists/*

COPY --from=uv /uv /uvx /usr/local/bin/

ENV UV_PROJECT_ENVIRONMENT=/opt/forcad \
    UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy

COPY backend/pyproject.toml backend/uv.lock ./

RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --locked --no-dev --no-install-project --python /usr/local/bin/python

COPY backend/src/ ./src/

RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --locked --no-dev --no-editable --python /usr/local/bin/python

FROM python-build AS checker-build

COPY checkers/requirements.txt /tmp/checker-requirements.txt

RUN uv venv /opt/checkers --python /usr/local/bin/python

RUN --mount=type=cache,target=/root/.cache/uv \
    uv pip install --python /opt/checkers/bin/python -r /tmp/checker-requirements.txt

FROM runtime-base AS runtime

COPY --from=python-build /opt/forcad /opt/forcad

RUN rm -rf \
        /usr/local/lib/python3.14/ensurepip \
        /usr/local/lib/python3.14/site-packages/pip \
        /usr/local/lib/python3.14/site-packages/pip-*.dist-info \
    && rm -f /usr/local/bin/pip /usr/local/bin/pip3 /usr/local/bin/pip3.14

COPY deployment/docker/scripts/await_start.sh /usr/local/bin/await_start
COPY deployment/docker/scripts/check_initialized.py /usr/local/bin/check_initialized.py
COPY deployment/docker/scripts/db_check.py /usr/local/bin/db_check.py
COPY deployment/docker/scripts/initializer-entrypoint.sh /usr/local/bin/initializer-entrypoint
COPY deployment/docker/scripts/stage_config.sh /usr/local/bin/stage_config

RUN chmod 0755 \
        /usr/local/bin/await_start \
        /usr/local/bin/check_initialized.py \
        /usr/local/bin/db_check.py \
        /usr/local/bin/initializer-entrypoint \
        /usr/local/bin/stage_config

ENV PATH="/opt/forcad/bin:${PATH}"

USER forcad:forcad

ENTRYPOINT ["/usr/local/bin/await_start"]
CMD ["api"]

FROM runtime AS worker

COPY --from=checker-build /opt/checkers /opt/checkers

ENV PATH="/opt/checkers/bin:/opt/forcad/bin:${PATH}"

FROM runtime AS initializer

ENTRYPOINT ["/usr/local/bin/initializer-entrypoint"]
CMD ["--fast"]

FROM python-build AS test

RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --locked --all-groups --no-editable --python /usr/local/bin/python

COPY backend/tests/ /app/backend/tests/
COPY config/config.yml.example /app/config/config.yml.example
COPY deployment/docker/compose*.yml /app/deployment/docker/
COPY scripts/run_unit_tests.py /app/scripts/run_unit_tests.py

ENV PATH="/opt/forcad/bin:${PATH}"

CMD ["/opt/forcad/bin/python", "/app/scripts/run_unit_tests.py"]

FROM runtime AS default

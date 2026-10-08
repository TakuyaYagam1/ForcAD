FROM python:3.11-slim-trixie

ENV PYTHONUNBUFFERED=1
ENV HOME=/home/forcad

COPY src/requirements.txt /requirements.txt
RUN apt-get update \
    && apt-get upgrade -y --no-install-recommends \
    && python -m pip install --no-cache-dir --upgrade \
        pip==26.2.1 setuptools==84.0.0 wheel==0.48.0 jaraco.context==6.1.2 \
    && python -m pip install --no-cache-dir -r /requirements.txt \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && python -m pip uninstall -y pip \
    && rm -rf /usr/local/lib/python3.11/ensurepip

COPY src /app

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /app
RUN groupadd --gid 10001 forcad \
    && useradd --uid 10001 --gid forcad --create-home --shell /usr/sbin/nologin forcad \
    && chown forcad:forcad /home/forcad \
    && chmod 0750 /home/forcad

USER forcad:forcad

CMD ["/entrypoint.sh"]

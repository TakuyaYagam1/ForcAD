FROM nats:2.15.2-alpine3.22@sha256:823e7f1d6d8ffe3b30dbd73d4a06bbb939acf94929f7fdf224404da7a05bafb5

RUN addgroup -g 10001 forcad \
    && adduser -D -H -u 10001 -G forcad forcad \
    && mkdir -p /data/jetstream \
    && chown -R 10001:10001 /data
COPY --chmod=0444 deployment/docker/nats/nats.conf /etc/nats/forcad.conf
USER 10001:10001
CMD ["nats-server", "--config", "/etc/nats/forcad.conf"]

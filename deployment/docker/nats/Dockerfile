FROM nats:2.15.3-alpine3.22@sha256:558394a23dd3c36f2d3a64b51865fd09908010e80a542cea5e75c2af543ed62a

RUN addgroup -g 10001 forcad \
    && adduser -D -H -u 10001 -G forcad forcad \
    && mkdir -p /data/jetstream \
    && chown -R 10001:10001 /data
COPY --chmod=0444 deployment/docker/nats/nats.conf /etc/nats/forcad.conf
USER 10001:10001
CMD ["nats-server", "--config", "/etc/nats/forcad.conf"]

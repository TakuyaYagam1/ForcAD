FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS build

RUN apk add --no-cache build-base libevent-dev openssl-dev c-ares-dev pkgconf
WORKDIR /src
RUN wget -O source.tar.gz https://www.pgbouncer.org/downloads/files/1.26.1/pgbouncer-1.26.1.tar.gz \
    && echo '00886a2994b4844cd37110970df777772cfc42a74c400522c82fba509a3f42d9  source.tar.gz' | sha256sum -c - \
    && tar -xzf source.tar.gz --strip-components=1 \
    && ./configure --prefix=/usr/local --with-cares --with-openssl --without-pam \
    && make -j2 pgbouncer

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk upgrade --no-cache \
    && apk add --no-cache libevent openssl c-ares postgresql18-client \
    && addgroup -g 10001 pgbouncer \
    && adduser -D -H -u 10001 -G pgbouncer pgbouncer
COPY --from=build /src/pgbouncer /usr/local/bin/pgbouncer
COPY --chmod=0755 deployment/docker/scripts/pgbouncer/start.sh /usr/local/bin/start-pgbouncer
COPY deployment/docker/scripts/pgbouncer/config.awk /usr/local/share/pgbouncer/config.awk
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/start-pgbouncer"]

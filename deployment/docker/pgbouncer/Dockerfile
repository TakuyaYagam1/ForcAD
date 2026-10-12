FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS build

RUN apk add --no-cache build-base libevent-dev openssl-dev c-ares-dev pkgconf
WORKDIR /src
RUN wget -O source.tar.gz https://www.pgbouncer.org/downloads/files/1.26.0/pgbouncer-1.26.0.tar.gz \
    && echo 'afd25dd61ee6775d37b40629b87ce08736b3e6955f3057bb212e410fbf21c71d  source.tar.gz' | sha256sum -c - \
    && tar -xzf source.tar.gz --strip-components=1 \
    && ./configure --prefix=/usr/local --with-cares --with-openssl --without-pam \
    && make -j2 pgbouncer

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk add --no-cache libevent openssl c-ares postgresql18-client \
    && addgroup -g 10001 pgbouncer \
    && adduser -D -H -u 10001 -G pgbouncer pgbouncer
COPY --from=build /src/pgbouncer /usr/local/bin/pgbouncer
COPY --chmod=0755 deployment/docker/scripts/pgbouncer/start.sh /usr/local/bin/start-pgbouncer
COPY deployment/docker/scripts/pgbouncer/config.awk /usr/local/share/pgbouncer/config.awk
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/start-pgbouncer"]

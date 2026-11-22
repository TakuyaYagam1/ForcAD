FROM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS gosu

WORKDIR /src
# Build gosu 1.20 from the release commit with the project's Go toolchain.
RUN wget -O gosu.tar.gz https://codeload.github.com/tianon/gosu/tar.gz/29b939aa5bd4dc588af765b8248f6e39150d2565 \
    && echo '2b70da8a5480fafedc16bb1fc91b572f064d03df736cbd91a616af2f1b66ab51  gosu.tar.gz' | sha256sum -c - \
    && tar -xzf gosu.tar.gz --strip-components=1 \
    && rm gosu.tar.gz \
    && CGO_ENABLED=0 go build -mod=readonly -trimpath -o /gosu .

FROM postgres:19.0-alpine3.24@sha256:8b0ff4cdf221f0c53d7fcda681d3b461be725c492cd881c5ec9c0ad25023f188

RUN apk upgrade --no-cache

COPY --from=gosu /gosu /usr/local/bin/gosu
COPY deployment/docker/scripts/postgres/prepare_data.sh /usr/local/bin/prepare-data
RUN chmod 0755 /usr/local/bin/gosu /usr/local/bin/prepare-data \
    && gosu --version \
    && gosu postgres id

USER postgres

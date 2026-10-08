FROM golang:1.27.2-alpine3.24 AS gosu

WORKDIR /src
# Upstream 1.19 ships a Go 1.24.6 binary. Keep its source, update the toolchain.
RUN wget -O gosu.tar.gz https://codeload.github.com/tianon/gosu/tar.gz/6456aaa0f3c854d199d0f037f068eb97515b7513 \
    && echo '33d7537d588ea49458b9509bcf4554bdf5ceacc66da71e5caa1058ea3b689c3b  gosu.tar.gz' | sha256sum -c - \
    && tar -xzf gosu.tar.gz --strip-components=1 \
    && rm gosu.tar.gz \
    && CGO_ENABLED=0 go build -mod=readonly -trimpath -o /gosu .

FROM postgres:17.11-alpine

RUN apk upgrade --no-cache
COPY --from=gosu /gosu /usr/local/bin/gosu
COPY docker_config/postgres/prepare_data.sh /usr/local/bin/prepare-data
RUN chmod 0755 /usr/local/bin/gosu /usr/local/bin/prepare-data \
    && gosu --version \
    && gosu postgres id

USER postgres

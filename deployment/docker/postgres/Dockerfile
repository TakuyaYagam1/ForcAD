FROM golang:1.27.3-alpine3.24@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS gosu

WORKDIR /src
# Upstream 1.19 ships a Go 1.24.6 binary. Keep its source, update the toolchain.
RUN wget -O gosu.tar.gz https://codeload.github.com/tianon/gosu/tar.gz/6456aaa0f3c854d199d0f037f068eb97515b7513 \
    && echo '33d7537d588ea49458b9509bcf4554bdf5ceacc66da71e5caa1058ea3b689c3b  gosu.tar.gz' | sha256sum -c - \
    && tar -xzf gosu.tar.gz --strip-components=1 \
    && rm gosu.tar.gz \
    && CGO_ENABLED=0 go build -mod=readonly -trimpath -o /gosu .

FROM postgres:18.6-alpine3.24@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873

RUN apk upgrade --no-cache

COPY --from=gosu /gosu /usr/local/bin/gosu
COPY deployment/docker/scripts/postgres/prepare_data.sh /usr/local/bin/prepare-data
RUN chmod 0755 /usr/local/bin/gosu /usr/local/bin/prepare-data \
    && gosu --version \
    && gosu postgres id

USER postgres

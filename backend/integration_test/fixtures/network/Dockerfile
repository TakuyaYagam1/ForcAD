FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

# Only test_network.sh runs this fixture, with NET_ADMIN in its own namespace.
RUN apk add --no-cache nftables

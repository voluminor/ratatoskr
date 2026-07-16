# syntax=docker/dockerfile:1.7
FROM golang:1.26.5-bookworm AS build

ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOBIN=/out go install github.com/yggdrasil-network/yggdrasil-go/cmd/yggdrasil@v0.5.14

FROM alpine:3.20
RUN apk add --no-cache jq ca-certificates
COPY --from=build /out/yggdrasil /usr/local/bin/yggdrasil
COPY tests/ygghub-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 7777
ENTRYPOINT ["/entrypoint.sh"]

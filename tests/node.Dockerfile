# syntax=docker/dockerfile:1.7
FROM golang:1.25-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
        bash ca-certificates curl jq iproute2 dnsutils netcat-openbsd procps \
 && rm -rf /var/lib/apt/lists/*

COPY tests/node-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]

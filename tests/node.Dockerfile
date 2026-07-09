# syntax=docker/dockerfile:1.7
# Development diagnostic node. The repository is mounted read-only at /src; the entrypoint copies it
# into /data/src and builds the test-only diagnostic binary there, so generated files and caches stay
# under tmp/tests rather than touching the host working tree.
FROM golang:1.25-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
        bash ca-certificates curl jq iproute2 dnsutils netcat-openbsd procps \
 && rm -rf /var/lib/apt/lists/*

COPY tests/node-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]

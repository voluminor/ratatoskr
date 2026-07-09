# syntax=docker/dockerfile:1.7
# Smoke verifier and manual diagnostics client.
FROM alpine:3.20

RUN apk add --no-cache bash curl jq ca-certificates coreutils python3
COPY tests/verifier/run-smoke.sh /run-smoke.sh
COPY tests/verifier/socks-udp-check.py /socks-udp-check.py
RUN chmod +x /run-smoke.sh && mkdir -p /out
WORKDIR /work
ENTRYPOINT ["/run-smoke.sh"]

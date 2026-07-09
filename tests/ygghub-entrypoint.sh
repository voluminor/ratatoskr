#!/bin/sh
# Generate an ephemeral no-TUN yggdrasil hub config and relay leaf-node peer traffic.
set -eu

CONF=/tmp/ygg.conf.json
LISTEN_PORT="${YGG_LISTEN_PORT:-7777}"
NAME="${YGG_NAME:-ygg-hub}"

PEERS_JSON='[]'
if [ -n "${YGG_PEERS:-}" ]; then
  PEERS_JSON=$(printf '%s' "$YGG_PEERS" | tr ', ' '\n\n' | grep -v '^$' | jq -R . | jq -cs .)
fi

yggdrasil -genconf -json > "$CONF"
jq --arg l "tcp://0.0.0.0:${LISTEN_PORT}" --arg n "$NAME" --argjson peers "$PEERS_JSON" \
   '.Listen=[$l] | .Peers=$peers | .IfName="none" | .AdminListen="none" | .MulticastInterfaces=[] | .NodeInfo={"name":$n}' \
   "$CONF" > "$CONF.tmp" && mv "$CONF.tmp" "$CONF"

echo "[${NAME}] listening tcp://0.0.0.0:${LISTEN_PORT}; peers=${YGG_PEERS:-<none>}"
exec yggdrasil -useconffile "$CONF" -loglevel info

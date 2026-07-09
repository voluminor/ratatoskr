#!/usr/bin/env bash
# Render disposable diagnostic configs under tmp/tests. No generated runtime state is committed.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TDIR="${ROOT}/tmp/tests"

mkdir -p \
  "${TDIR}/cache/go-build" \
  "${TDIR}/cache/go-mod" \
  "${TDIR}/cache/gopath" \
  "${TDIR}/results" \
  "${TDIR}/node-a/results" \
  "${TDIR}/node-b/results" \
  "${TDIR}/node-c/results"

write_config() {
  local node=$1 hub=$2
  cat > "${TDIR}/${node}/config.json" <<EOF
{
  "name": "${node}",
  "peers": ["tcp://${hub}:7777"],
  "if_mtu": 65535,
  "http_listen": "0.0.0.0:8080",
  "debug_listen": "0.0.0.0:7070",
  "debug_enabled": true,
  "socks_listen": "0.0.0.0:1080",
  "socks_max_connections": 2,
  "tcp_echo_port": 80,
  "udp_echo_port": 18081,
  "results_dir": "/data/results",
  "core_stop_timeout": "10s"
}
EOF
}

write_config node-a ygg-hub-1
write_config node-b ygg-hub-1
write_config node-c ygg-hub-2

cat > "${TDIR}/topology.txt" <<EOF
ratatoskr diagnostic topology

ygg-hub-2 -> tcp://ygg-hub-1:7777
node-a    -> tcp://ygg-hub-1:7777
node-b    -> tcp://ygg-hub-1:7777
node-c    -> tcp://ygg-hub-2:7777

host ports:
  node-a HTTP  http://127.0.0.1:18080   pprof http://127.0.0.1:16080/debug/pprof/
  node-b HTTP  http://127.0.0.1:18081   pprof http://127.0.0.1:16081/debug/pprof/
  node-c HTTP  http://127.0.0.1:18082   pprof http://127.0.0.1:16082/debug/pprof/
  SOCKS host ports: 11080, 11081, 11082
EOF

echo "[bootstrap] wrote configs and topology under ${TDIR}"

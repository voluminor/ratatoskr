#!/usr/bin/env bash
# Smoke checks for the live ratatoskr diagnostic stack. Hard failures mean the harness itself or a
# basic data path is broken; large-payload checks are recorded as diagnostics for MTU/UDP work.
set -Eeuo pipefail

OUT="${OUT:-/out}"
mkdir -p "$OUT"
RESULTS="$OUT/smoke-results.txt"
: > "$RESULTS"

log() {
  echo "$*" | tee -a "$RESULTS"
}

get_json() {
  curl -fsS --max-time "${3:-10}" "http://$1:$2"
}

post_json() {
  curl -fsS --max-time "${4:-20}" -H 'Content-Type: application/json' -d "$3" "http://$1:$2"
}

wait_health() {
  local node=$1 deadline=$((SECONDS + 240))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if get_json "$node" "8080/health" 3 > "$OUT/${node}-health.json" 2>/dev/null; then
      log "[smoke] ${node} health OK"
      return 0
    fi
    sleep 2
  done
  log "[smoke] ${node} health FAILED"
  return 1
}

wait_peer() {
  local node=$1 deadline=$((SECONDS + 120))
  while [ "$SECONDS" -lt "$deadline" ]; do
    get_json "$node" "8080/snapshot" 5 > "$OUT/${node}-snapshot.json" || true
    if jq -e '[.peers[]? | select(.up == true)] | length > 0' "$OUT/${node}-snapshot.json" >/dev/null 2>&1; then
      log "[smoke] ${node} has an Up peer"
      return 0
    fi
    sleep 2
  done
  log "[smoke] ${node} peer wait FAILED"
  return 1
}

hard_fail=0
soft_fail=0

hard() {
  local label=$1
  shift
  if "$@"; then
    log "[hard] PASS ${label}"
  else
    log "[hard] FAIL ${label}"
    hard_fail=$((hard_fail + 1))
  fi
}

soft() {
  local label=$1
  shift
  if "$@"; then
    log "[soft] PASS ${label}"
  else
    log "[soft] FAIL ${label}"
    soft_fail=$((soft_fail + 1))
  fi
}

json_ok() {
  jq -e '.ok == true' "$1" >/dev/null
}

retry_json_ok() {
  local out=$1 deadline=$((SECONDS + ${2:-60}))
  shift 2
  while [ "$SECONDS" -lt "$deadline" ]; do
    "$@" > "$out" 2>/dev/null || true
    if json_ok "$out"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

retry_check() {
  local deadline=$((SECONDS + ${1:-60}))
  shift
  while [ "$SECONDS" -lt "$deadline" ]; do
    if "$@"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

check_socks_pk_ygg() {
  local pk url
  pk=$(jq -r '.public_key' "$OUT/node-b-health.json")
  url="http://${pk}.pk.ygg:80/ratatoskr-socks-ygg"
  rm -f "$OUT/socks-pk-ygg.txt" "$OUT/socks-pk-ygg-curl.err"
  curl -fsS --http0.9 --socks5-hostname node-a:1080 --max-time 3 "$url" > "$OUT/socks-pk-ygg.txt" 2> "$OUT/socks-pk-ygg-curl.err" || true
  grep -q "GET /ratatoskr-socks-ygg" "$OUT/socks-pk-ygg.txt"
}

check_socks_udp_ygg() {
  local udp_b payload
  udp_b=$(jq -r '.udp_echo_addr' "$OUT/node-b-health.json")
  payload="ratatoskr-socks-udp"
  rm -f "$OUT/socks-udp-ygg.txt" "$OUT/socks-udp-ygg.err"
  python3 /socks-udp-check.py node-a 1080 "$udp_b" "$payload" > "$OUT/socks-udp-ygg.txt" 2> "$OUT/socks-udp-ygg.err" || true
  grep -q "$payload" "$OUT/socks-udp-ygg.txt"
}

run_smoke() {
  log "[smoke] starting"
  for n in node-a node-b node-c; do
    hard "health ${n}" wait_health "$n"
  done
  for n in node-a node-b node-c; do
    hard "peer ${n}" wait_peer "$n"
  done

  local addr_b addr_c tcp_b udp_b
  addr_b=$(jq -r '.address' "$OUT/node-b-health.json")
  addr_c=$(jq -r '.address' "$OUT/node-c-health.json")
  tcp_b=$(jq -r '.tcp_echo_addr' "$OUT/node-b-health.json")
  udp_b=$(jq -r '.udp_echo_addr' "$OUT/node-b-health.json")
  log "[smoke] node-b=${addr_b} node-c=${addr_c}"

  hard "TCP echo node-a -> node-b" retry_json_ok "$OUT/tcp-small.json" 60 \
    post_json node-a "8080/check/tcp" "{\"address\":\"${tcp_b}\",\"payload\":\"ratatoskr-smoke\",\"timeout_ms\":5000}" 15

  hard "UDP echo 512B node-a -> node-b" retry_json_ok "$OUT/udp-small.json" 60 \
    post_json node-a "8080/check/udp" "{\"address\":\"${udp_b}\",\"size\":512,\"timeout_ms\":5000}" 15

  soft "UDP echo 4096B diagnostic" retry_json_ok "$OUT/udp-4096.json" 60 \
    post_json node-a "8080/check/udp" "{\"address\":\"${udp_b}\",\"size\":4096,\"timeout_ms\":5000}" 15

  post_json node-a "8080/socks/enable" '{}' 10 > "$OUT/socks-enable.json" || true
  hard "SOCKS enable" json_ok "$OUT/socks-enable.json"
  hard "SOCKS .pk.ygg to IPv6 port 80" retry_check 60 check_socks_pk_ygg
  hard "SOCKS UDP echo node-a -> node-b" retry_check 60 check_socks_udp_ygg
  post_json node-a "8080/socks/disable" '{}' 10 > "$OUT/socks-disable.json" || true
  hard "SOCKS disable" json_ok "$OUT/socks-disable.json"

  get_json node-a "7070/debug/pprof/goroutine?debug=1" 10 > "$OUT/node-a-goroutine.txt"
  hard "pprof goroutine" test -s "$OUT/node-a-goroutine.txt"

  post_json node-a "8080/load/tcp" "{\"address\":\"${tcp_b}\",\"size\":1024,\"seconds\":5,\"streams\":4,\"timeout_ms\":5000}" 20 > "$OUT/load-tcp.json" &
  local load_pid=$!
  sleep 1
  curl -fsS --max-time 10 "http://node-a:7070/debug/pprof/profile?seconds=2" -o "$OUT/node-a-cpu.pprof"
  curl -fsS --max-time 10 "http://node-a:7070/debug/pprof/trace?seconds=1" -o "$OUT/node-a-trace.out"
  wait "$load_pid" || true
  soft "TCP load completed" json_ok "$OUT/load-tcp.json"
  hard "CPU profile captured" test -s "$OUT/node-a-cpu.pprof"
  hard "runtime trace captured" test -s "$OUT/node-a-trace.out"

  for n in node-a node-b node-c; do
    get_json "$n" "8080/runtime" 10 > "$OUT/${n}-runtime.json" || true
  done

  log "[smoke] hard_fail=${hard_fail} soft_fail=${soft_fail}"
  if [ "$hard_fail" -ne 0 ]; then
    return 1
  fi
  return 0
}

case "${1:-smoke}" in
  smoke)
    run_smoke
    ;;
  health)
    wait_health "${2:-node-a}"
    ;;
  pprof)
    node="${2:-node-a}"
    get_json "$node" "7070/debug/pprof/goroutine?debug=1"
    ;;
  *)
    echo "usage: run-smoke.sh [smoke|health NODE|pprof NODE]" >&2
    exit 2
    ;;
esac

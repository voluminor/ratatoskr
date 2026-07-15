#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

export DOCKER_BUILDKIT=1
export HOST_UID="$(id -u)"
export HOST_GID="$(id -g)"
export RTS_REBUILD=1

COMPOSE=(docker compose -f tests/docker-compose.yml)
build=1
verify=0
throughput=0
keep_state=0

for arg in "$@"; do
  case "$arg" in
    --no-build) build=0 ;;
    --verify) verify=1 ;;
    --throughput) throughput=1 ;;
    --keep-state) keep_state=1 ;;
    --no-rebuild) export RTS_REBUILD=0 ;;
    *)
      echo "unknown flag: ${arg}" >&2
      exit 2
      ;;
  esac
done

if [ "${verify}" = 1 ] && [ "${throughput}" = 1 ]; then
  echo "--verify and --throughput are mutually exclusive" >&2
  exit 2
fi

cleanup_after_verify() {
  local rc=$?
  if [ "$verify" = 1 ] || [ "$throughput" = 1 ]; then
    "${COMPOSE[@]}" --profile verify down --remove-orphans || true
    if [ "$keep_state" = 0 ]; then
      chmod -R u+w "${ROOT}/tmp/tests" 2>/dev/null || true
      rm -rf "${ROOT}/tmp/tests"
      echo "[up] removed tmp/tests state (--keep-state not set)"
    else
      echo "[up] kept tmp/tests for inspection"
    fi
  fi
  exit "$rc"
}

if [ "$verify" = 1 ] || [ "$throughput" = 1 ]; then
  trap cleanup_after_verify EXIT
fi

if [ "$build" = 1 ]; then
  echo "[up] building diagnostic images"
  docker build -f tests/ygghub.Dockerfile -t rts-ygghub:latest .
  docker build -f tests/node.Dockerfile -t rts-node:latest .
  docker build -f tests/verifier.Dockerfile -t rts-verifier:latest .
fi

bash tests/scripts/bootstrap.sh

echo "[up] starting Yggdrasil hubs and ratatoskr diagnostic nodes"
"${COMPOSE[@]}" up -d ygg-hub-1 ygg-hub-2 node-a node-b node-c
"${COMPOSE[@]}" ps

if [ "$verify" = 1 ]; then
  echo "[up] running smoke verifier"
  "${COMPOSE[@]}" run --rm verifier
elif [ "$throughput" = 1 ]; then
  echo "[up] running throughput diagnostics"
  "${COMPOSE[@]}" run --rm verifier throughput
else
  echo "[up] stack is running"
  echo "[up] topology: tmp/tests/topology.txt"
  echo "[up] stop: bash tests/scripts/down.sh"
  echo "[up] clean stop: bash tests/scripts/down.sh --clean"
fi

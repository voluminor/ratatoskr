#!/usr/bin/env bash
# Stop/remove the diagnostic stack. Add --clean for tmp/tests, --prune for images and BuildKit cache.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
clean=0
prune=0

for arg in "$@"; do
  case "$arg" in
    --clean) clean=1 ;;
    --prune) prune=1 ;;
    *)
      echo "unknown flag: ${arg}" >&2
      exit 2
      ;;
  esac
done

docker compose -f "${ROOT}/tests/docker-compose.yml" --profile verify down --remove-orphans

if [ "$clean" = 1 ]; then
  chmod -R u+w "${ROOT}/tmp/tests" 2>/dev/null || true
  rm -rf "${ROOT}/tmp/tests"
  echo "[down] removed tmp/tests"
fi

if [ "$prune" = 1 ]; then
  docker image rm -f rts-node:latest rts-ygghub:latest rts-verifier:latest 2>/dev/null || true
  docker builder prune -f >/dev/null 2>&1 || true
  echo "[down] removed test images and pruned BuildKit cache"
fi

echo "[down] stopped"

#!/usr/bin/env bash
# Build and run the ratatoskr diagnostic node entirely from tmp/tests-mounted state.
set -Eeuo pipefail

SRC="${SRC:-/src}"
DATA="${DATA:-/data}"
WORK="${DATA}/src"
BIN="${DATA}/bin/ratatoskr-diag"
CONFIG="${CONFIG:-/data/config.json}"

export HOME="${HOME:-${DATA}/home}"
export GOCACHE="${GOCACHE:-/cache/go-build}"
export GOMODCACHE="${GOMODCACHE:-/cache/go-mod}"
export GOPATH="${GOPATH:-/cache/gopath}"
export GOFLAGS="${GOFLAGS:-} -modcacherw"

mkdir -p "${DATA}/bin" "${DATA}/results" "${HOME}" "${GOCACHE}" "${GOMODCACHE}" "${GOPATH}"

if [ "${RTS_REBUILD:-1}" = "1" ] || [ ! -x "${BIN}" ]; then
  echo "[node-entrypoint] rebuilding diagnostic binary into ${BIN}"
  rm -rf "${WORK}"
  mkdir -p "${WORK}"
  tar -C "${SRC}" \
    --exclude='./.git' \
    --exclude='./.idea' \
    --exclude='./tmp' \
    -cf - . | tar -C "${WORK}" -xf -
  cd "${WORK}"
  mkdir -p target tmp
  go generate .
  GOWORK=off go build -mod=mod -trimpath -o "${BIN}" ./tests/diag
else
  echo "[node-entrypoint] reusing ${BIN}"
fi

exec "${BIN}" -config "${CONFIG}"

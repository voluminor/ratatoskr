#!/usr/bin/env bash
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
  root_work="${DATA}/root-work"
  mkdir -p "${root_work}"
  rm -f "${root_work}/go.work" "${root_work}/go.work.sum"
  GOWORK=off go -C "${root_work}" work init "${WORK}" "${WORK}/_generate/sigils"
  GOWORK="${root_work}/go.work" go generate .
  GOWORK=off go build -mod=mod -trimpath -o "${BIN}" ./tests/diag
else
  echo "[node-entrypoint] reusing ${BIN}"
fi

exec "${BIN}" -config "${CONFIG}"

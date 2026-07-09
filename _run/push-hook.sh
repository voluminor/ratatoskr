#!/usr/bin/env bash

set -Eeuo pipefail

echo "[HOOK]" "Push"

run_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root_path="$(cd "$run_dir/.." && pwd)"

#############################################################################

(
  cd "$root_path"
  export CGO_ENABLED=1

  go work sync
  go generate .

  go list -m -f '{{if .Main}}{{.Dir}}{{end}}' all | while read -r dir; do
    [ -n "$dir" ] || continue
    echo "  -> $dir"
    go -C "$dir" mod tidy
  done

  echo "==> Running tests with race detector..."
  go test -race -v ./...

  echo ""
  echo "==> Running benchmarks..."
  go test -bench=. -run=NONE -benchmem -v ./...
)

echo ""
echo "[HOOK] All tests and benchmarks passed"

#############################################################################
exit 0

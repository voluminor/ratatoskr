#!/bin/bash
echo "[HOOK]" "Push"

run_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
values_dir="$run_dir/values"
script_dir="$run_dir/scripts"
root_path=$(cd "$run_dir/.." && pwd)

#############################################################################

set -Eeuo pipefail
cd "$root_path"
export CGO_ENABLED=1

go work sync
go generate .
./_run/scripts/go_tidy_all.sh

echo "==> Running tests with race detector..."
go test -race -v ./...

echo ""
echo "==> Running benchmarks..."
go test -bench=. -run=NONE -benchmem -v ./...

echo ""
echo "[HOOK] All tests and benchmarks passed"

#############################################################################
exit 0

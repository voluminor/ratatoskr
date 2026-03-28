#!/bin/bash
set -Eeuo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# go work sync обновляет go.sum для всех модулей в workspace
cd "$root_dir"
go work sync

# go mod tidy for each module (skip _generate — internal tooling with unpublishable imports)
while IFS= read -r mod_dir; do
    dir=$(dirname "$mod_dir")
    echo "tidy: $dir"
    (cd "$dir" && go mod tidy)
done < <(find "$root_dir" -name 'go.mod' -not -path '*/vendor/*' -not -path '*/_generate/*')

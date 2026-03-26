#!/bin/bash
echo "[HOOK]" "Push"

run_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
values_dir="$run_dir/values"
script_dir="$run_dir/scripts"
root_path=$(cd "$run_dir/.." && pwd)

#############################################################################

bash "$script_dir/go_tidy_all.sh"

OLD_VER=$(bash "$script_dir/sys.sh" -v)
VERSION=$(bash "$script_dir/sys.sh" -i -pa)

bash "$script_dir/go_creator_const.sh"

echo "Updated patch-ver:" "$OLD_VER >> $VERSION"

#############################################################################
exit 0


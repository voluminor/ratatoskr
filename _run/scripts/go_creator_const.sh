#!/bin/bash

dir_path=""
file_name="target/value_project.go"
package_name="target"

#############################################################################
#############################################################################

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
values_dir="$(dirname "$script_dir")"
root_path="$(dirname "$values_dir")"
values_dir="$values_dir/values"

DATE_NOW=$(date +"%m-%d-%Y")
NAME=$(bash "$script_dir/sys.sh" -n)
HASH=$(bash "$script_dir/git.sh" -h)

VERSION=$(bash "$script_dir/sys.sh" -v)
VERSION_MAJOR=$(bash "$script_dir/sys.sh" -ma)
VERSION_MINOR=$(bash "$script_dir/sys.sh" -mi)
VERSION_PATCH=$(bash "$script_dir/sys.sh" -pa)

#############################################################################
          ################[ File generation ]################

file_const="$root_path/$dir_path$file_name"

echo "// Code generated using '_run/scripts/go_creator_const.sh'; DO NOT EDIT." > "$file_const"
echo "package $package_name" >> "$file_const"

echo "" >> "$file_const"
echo "const (" >> "$file_const"
echo -e "\t GlobalName string = \"$NAME\"" >> "$file_const"
echo -e "\t GlobalDateUpdate string = \"$DATE_NOW\"" >> "$file_const"
echo -e "\t GlobalHash string = \"$HASH\"" >> "$file_const"

echo "" >> "$file_const"
echo -e "\t GlobalVersion string = \"$VERSION\"" >> "$file_const"
echo -e "\t GlobalVersionMajor string = \"$VERSION_MAJOR\"" >> "$file_const"
echo -e "\t GlobalVersionMinor uint16 = $VERSION_MINOR" >> "$file_const"
echo -e "\t GlobalVersionPatch uint16 = $VERSION_PATCH" >> "$file_const"
echo ")" >> "$file_const"

exit 0